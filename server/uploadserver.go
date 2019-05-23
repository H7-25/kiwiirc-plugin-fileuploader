package server

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	
	

	"github.com/gin-gonic/gin"
	"github.com/kiwiirc/plugin-fileuploader/db"
	"github.com/kiwiirc/plugin-fileuploader/events"
	"github.com/kiwiirc/plugin-fileuploader/expirer"
	"github.com/kiwiirc/plugin-fileuploader/logging"
	"github.com/kiwiirc/plugin-fileuploader/shardedfilestore"
)

// UploadServer is a simple configurable service for file sharing.
// Compatible with TUS upload clients.
type UploadServer struct {
	cfg                 Config
	DBConn              *db.DatabaseConnection
	store               *shardedfilestore.ShardedFileStore
	Router              *gin.Engine
	expirer             *expirer.Expirer
	httpServer          *http.Server
	startedMu           sync.Mutex
	started             chan struct{}
	tusEventBroadcaster *events.TusEventBroadcaster
}

// GetStartedChan returns a channel that will close when the server startup is complete
func (serv *UploadServer) GetStartedChan() chan struct{} {
	serv.startedMu.Lock()
	defer serv.startedMu.Unlock()

	if serv.started == nil {
		serv.started = make(chan struct{})
	}

	return serv.started
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// Run starts the UploadServer
func (serv *UploadServer) Run(replaceableHandler *ReplaceableHandler) error {
	serv.Router = gin.New()
	serv.Router.Use(logging.GinLogger(), gin.Recovery())

	serv.DBConn = db.ConnectToDB(db.DBConfig{
		DriverName: serv.cfg.Database.Type,
		DSN:        serv.cfg.Database.Path,
	})

	serv.store = shardedfilestore.New(
		serv.cfg.Storage.Path,
		serv.cfg.Storage.ShardLayers,
		serv.DBConn,
	)

	serv.expirer = expirer.New(
		serv.store,
		serv.cfg.Expiration.MaxAge.Duration,
		serv.cfg.Expiration.IdentifiedMaxAge.Duration,
		serv.cfg.Expiration.CheckInterval.Duration,
		serv.cfg.JwtSecretsByIssuer,
	)

	err := serv.registerTusHandlers(serv.Router, serv.store)
	if err != nil {
		return err
	}

	// closed channel indicates that startup is complete
	close(serv.GetStartedChan())

	if replaceableHandler != nil {
		// set ReplaceableHandler that's mounted in an external server
		replaceableHandler.Handler = serv.Router
		return nil
	}

	// otherwise run our own http server
	if strings.HasPrefix(strings.ToLower(serv.cfg.Server.ListenAddress), "unix:") {
		socketFile := serv.cfg.Server.ListenAddress[5:]
		server, serverErr := net.Listen("unix", socketFile)
		if serverErr != nil {
			return serverErr
		}

		// parse the mode string and chmod the sock
		intMode, err := strconv.ParseInt(serv.cfg.Server.BindMode, 8, 32)
		if err != nil {
			intMode = 0755
		}
		bindMode := os.FileMode(intMode)
		os.Chmod(socketFile, bindMode)

		return http.Serve(server, serv.Router)
	} else {
		serv.httpServer = &http.Server{
			Addr:    serv.cfg.Server.ListenAddress,
			Handler: serv.Router,
		}

		return serv.httpServer.ListenAndServe()
	}
}

// Shutdown gracefully terminates the UploadServer instance.
// The HTTP listen socket will close immediately, causing the .Run() call to return.
// The call to .Shutdown() will block until all outstanding requests have been served and
// other resources like database connections and timers have been closed and stopped.
func (serv *UploadServer) Shutdown() {
	// wait for startup to complete
	<-serv.GetStartedChan()

	// wait for all requests to finish
	if serv.httpServer != nil {
		serv.httpServer.Shutdown(nil)
	}

	// stop running FileStore GC cycles
	serv.expirer.Stop()

	// close db connections
	serv.DBConn.DB.Close()

	// close event broadcaster
	serv.tusEventBroadcaster.Close()
}
