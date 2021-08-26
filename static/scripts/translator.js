/**
 * simple-translator
 * A small JavaScript library to translate webpages into different languages.
 * https://github.com/andreasremdt/simple-translator
 *
 * Author: Andreas Remdt <me@andreasremdt.com> (https://andreasremdt.com)
 * License: MIT (https://mit-license.org/)
 */
class Translator {
  /**
   * Initialize the Translator by providing options.
   *
   * @param {Object} options
   */
  constructor(options = {}) {

    if (typeof options != 'object' || Array.isArray(options)) {
      this.debug('INVALID_OPTIONS', options);
      options = {};
    }

    this.languages = new Map();
    this.config = Object.assign(Translator.defaultConfig, options);

    const { debug, registerGlobally, detectLanguage } = this.config;


    if (registerGlobally) {
      this._globalObject[registerGlobally] = this.translateForKey.bind(this);
    }

    if (detectLanguage && this._env == 'browser') {
      this._detectLanguage();
    }
  }

  /**
   * Return the global object, depending on the environment.
   * If the script is executed in a browser, return the window object,
   * otherwise, in Node.js, return the global object.
   *
   * @return {Object}
   */
  get _globalObject() {
    if (this._env == 'browser') {
      return window;
    }

    return global;
  }

  /**
   * Check and return the environment in which the script is executed.
   *
   * @return {String} The environment
   */
  get _env() {
    if (typeof window != 'undefined') {
      return 'browser';
    } else if (typeof module !== 'undefined' && module.exports) {
      return 'node';
    }

    return 'browser';
  }

  /**
   * Detect the users preferred language. If the language is stored in
   * localStorage due to a previous interaction, use it.
   * If no localStorage entry has been found, use the default browser language.
   */
  _detectLanguage() {
    const inMemory = localStorage.getItem(this.config.persistKey);

    if (inMemory) {
      this.config.defaultLanguage = inMemory;
    } else {
      const lang = navigator.languages
        ? navigator.languages[0]
        : navigator.language;

      this.config.defaultLanguage = lang.substr(0, 2);
    }
  }

  /**
   * Get a translated value from a JSON by providing a key. Additionally,
   * the target language can be specified as the second parameter.
   *
   * @param {String} key
   * @param {String} toLanguage
   * @return {String}
   */
  _getValueFromJSON(key, toLanguage) {
    const json = this.languages.get(toLanguage);

    return key.split('.').reduce((obj, i) => (obj ? obj[i] : null), json);
  }

  /**
   * Replace a given DOM nodes' attribute values (by default innerHTML) with
   * the translated text.
   *
   * @param {HTMLElement} element
   * @param {String} toLanguage
   */
  _replace(element, toLanguage) {
    const keys = element.getAttribute('data-i18n')?.split(/\s/g);
    const attributes = element?.getAttribute('data-i18n-attr')?.split(/\s/g);


    keys.forEach((key, index) => {
      const text = this._getValueFromJSON(key, toLanguage);
      const attr = attributes ? attributes[index] : 'innerHTML';

      if (text) {
        if (attr == 'innerHTML') {
          element[attr] = text;
        } else {
          element.setAttribute(attr, text);
        }
      }
    });
  }

  /**
   * Translate all DOM nodes that match the given selector into the
   * specified target language.
   *
   * @param {String} toLanguage The target language
   */
  translatePageTo(toLanguage = this.config.defaultLanguage) {

    const elements =
      typeof this.config.selector == 'string'
        ? Array.from(document.querySelectorAll(this.config.selector))
        : this.config.selector;

    if (elements.length && elements.length > 0) {
      elements.forEach((element) => this._replace(element, toLanguage));
    } else if (elements.length == undefined) {
      this._replace(elements, toLanguage);
    }

    this._currentLanguage = toLanguage;
    document.documentElement.lang = toLanguage;

    if (this.config.persist) {
      localStorage.setItem(this.config.persistKey, toLanguage);
    }
  }

  /**
   * Translate a given key into the specified language if it exists
   * in the translation file. If not or if the language hasn't been added yet,
   * the return value is `null`.
   *
   * @param {String} key The key from the language file to translate
   * @param {String} toLanguage The target language
   * @return {(String|null)}
   */
  translateForKey(key, toLanguage = this.config.defaultLanguage) {

    return text;
  }

  /**
   * Add a translation resource to the Translator object. The language
   * can then be used to translate single keys or the entire page.
   *
   * @param {String} language The target language to add
   * @param {String} json The language resource file as JSON
   * @return {Object} Translator instance
   */
  add(language, json) {


    this.languages.set(language, json);

    return this;
  }

  /**
   * Remove a translation resource from the Translator object. The language
   * won't be available afterwards.
   *
   * @param {String} language The target language to remove
   * @return {Object} Translator instance
   */
  remove(language) {

    this.languages.delete(language);

    return this;
  }

  /**
   * Fetch a translation resource from the web server. It can either fetch
   * a single resource or an array of resources. After all resources are fetched,
   * return a Promise.
   * If the optional, second parameter is set to true, the fetched translations
   * will be added to the Translator object.
   *
   * @param {String|Array} sources The files to fetch
   * @param {Boolean} save Save the translation to the Translator object
   * @return {(Promise|null)}
   */
  fetch(sources, save = true) {

    if (!Array.isArray(sources)) {
      sources = [sources];
    }

    const urls = sources.map((source) => {
      const filename = source.replace(/\.json$/, '').replace(/^\//, '');
      const path = this.config.filesLocation.replace(/\/$/, '');

      return `${path}/${filename}.json`;
    });

    if (this._env == 'browser') {
      return Promise.all(urls.map((url) => fetch(url)))
        .then((responses) =>
          Promise.all(
            responses.map((response) => {
              if (response.ok) {
                return response.json();
              }

            })
          )
        )
        .then((languageFiles) => {
          // If a file could not be fetched, it will be `undefined` and filtered out.
          languageFiles = languageFiles.filter((file) => file);

          if (save) {
            languageFiles.forEach((file, index) => {
              this.add(sources[index], file);
            });
          }

          return languageFiles.length > 1 ? languageFiles : languageFiles[0];
        });
    } else if (this._env == 'node') {
      return new Promise((resolve) => {
        const languageFiles = [];

        urls.forEach((url, index) => {
          try {
            const json = JSON.parse(
              require('fs').readFileSync(process.cwd() + url, 'utf-8')
            );

            if (save) {
              this.add(sources[index], json);
            }

            languageFiles.push(json);
          } catch (err) {

          }
        });

        resolve(languageFiles.length > 1 ? languageFiles : languageFiles[0]);
      });
    }
  }

  /**
   * Sets the default language of the translator instance.
   *
   * @param {String} language
   * @return {void}
   */
  setDefaultLanguage(language) {
  
    this.config.defaultLanguage = language;
  }

  /**
   * Return the currently selected language.
   *
   * @return {String}
   */
  get currentLanguage() {
    return this._currentLanguage || this.config.defaultLanguage;
  }

  /**
   * Returns the current default language;
   *
   * @return {String}
   */
  get defaultLanguage() {
    return this.config.defaultLanguage;
  }

  /**
   * Return the default config object whose keys can be overriden
   * by the user's config passed to the constructor.
   *
   * @return {Object}
   */
  static get defaultConfig() {
    return {
      defaultLanguage: 'en',
      detectLanguage: true,
      selector: '[data-i18n]',
      registerGlobally: '__',
      persist: false,
      persistKey: 'preferred_language',
      filesLocation: '/i18n',
    };
  }
}
