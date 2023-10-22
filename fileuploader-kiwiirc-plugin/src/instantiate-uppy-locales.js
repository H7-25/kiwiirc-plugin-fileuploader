import Translator from '@uppy/utils/lib/Translator';
import DefaultLang from '@uppy/locales/lib/en_US.js';

import * as config from '@/config.js';

/* global _:true */

export default function instantiateUppyLocales(kiwiApi, uppy) {
    function loadLocale(_lang) {
        let lang = (_lang || kiwiApi.i18n.language).split('-');
        if (lang.length !== 2) {
            setDefaultLocale();
            return;
        }

        const langUppy = lang.join('_').toLowerCase();
        const localePathConfig = config.getSetting('localePath');
        const localePathAuto = config.getBasePath() + 'plugin-fileuploader/locales/uppy/%LANG%.json';
        const localePath = localePathConfig || localePathAuto;
        const localeURL = localePath.replace('%LANG%', langUppy);

        fetch(localeURL)
            .then((r) => {
                if (r.status !== 200) {
                    console.error('failed to load uppy locale, path:', localeURL);
                    setDefaultLocale();
                    return;
                }
                return r.json();
            })
            .then((j) => {
                const locale = _.cloneDeep(DefaultLang);
                Object.assign(locale.strings, j);
                setLocale(locale);
            }).catch(() => {
                setDefaultLocale();
            });
    }

    function setDefaultLocale() {
        setLocale(DefaultLang);
    }

    function setLocale(locale) {
        // update uppy core
        uppy.opts.locale = locale;
        uppy.translator = new Translator([DefaultLang, uppy.opts.locale]);
        uppy.locale = uppy.translator.locale;
        uppy.i18n = uppy.translator.translate.bind(uppy.translator);
        uppy.i18nArray = uppy.translator.translateArray.bind(uppy.translator);
        uppy.setState();

        // update uppy plugins
        uppy.iteratePlugins((plugin) => {
            if (plugin.translator) {
                plugin.translator = uppy.translator;
            }
            if (plugin.i18n) {
                plugin.i18n = uppy.i18n;
            }
            if (plugin.i18nArray) {
                plugin.i18nArray = uppy.i18nArray;
            }
            plugin.setPluginState();
        });
    }

    loadLocale();

    kiwiApi.Vue.watch(
        kiwiApi.state.settingComputed('language'),
        (lang) => loadLocale(lang),
    );
};