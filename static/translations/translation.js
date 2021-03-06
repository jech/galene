// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.  IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

'use strict';

/**
 *
 * @constructor
 */
function Translation() {
    /**
     * Default language
     * @type {string}
     */
    this.default = 'en';
    /**
     * Used language
     * @type {string|string}
     */
    this.language = this.default;
    /**
     * Available languages
     * @type {string[]}
     */
    this.languages = ['en', 'fr'];
    /**
     * Json witch contains translations
     * @type {undefined}
     */
    this.json = undefined;

    this.selectLanguage(navigator.language);
}

/**
 *
 * @returns {Promise<any>}
 */
Translation.prototype.load = async function (){
    const url = window.location.origin + '/translations/i18n.' + this.language + '.json';
    return await (await fetch(url)).json();
}
/**
 * Analyse all HTML to remplace innerHTML with value linked to the key
 * @returns {Promise<void>}
 */
Translation.prototype.analyse = async function (){
    this.json = await trans.load();
    document.querySelectorAll('[data-i18n]').forEach(item => {
        item.innerHTML = this.get(item.getAttribute('data-i18n'))
    });

    document.querySelectorAll('[data-i18n-title]').forEach(item => {
        item.title = this.get(item.getAttribute('data-i18n-title'))
    });

    document.querySelectorAll('[data-i18n-value]').forEach(item => {
        item.value = this.get(item.getAttribute('data-i18n-value'))
    });
}

/**
 *
 * @param key
 * @returns string
 */
Translation.prototype.get = function(key) {
    return key in this.json ? this.json[key] : key;
};

/**
 * Select an available language
 * @param language
 */
Translation.prototype.selectLanguage = function (language){
    this.language = this.languages.filter(l => l === language).length === 1 ? language : this.default;
}
