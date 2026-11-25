// Copyright (c) 2026 by the Galene contributors.

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
 * The locale used when no translation is available.
 * @type {string}
 */
const i18nDefaultLocale = 'en';

/**
 * The locale requested through the ?lang query parameter.
 * @type {string|null}
 */
const i18nParamLang = new URLSearchParams(window.location.search).get('lang');

/**
 * The active locale.
 * @type {string}
 */
let i18nLocale = i18nDefaultLocale;

/**
 * The base (English) catalog.
 * @type {Object.<string,string>}
 */
let i18nBase = {};

/**
 * The catalog for the active locale.
 * @type {Object.<string,string>}
 */
let i18nMessages = {};

/**
 * i18nCandidates returns the list of locale tags to try, in order of
 * preference.
 *
 * @returns {string[]}
 */
function i18nCandidates() {
    /** @type {string[]} */
    let candidates = [];

    /**
     * @param {string} tag
     */
    function add(tag) {
        if(!tag)
            return;
        if(candidates.indexOf(tag) >= 0)
            return;
        candidates.push(tag);
        let base = tag.split('-')[0];
        if(base !== tag && candidates.indexOf(base) < 0)
            candidates.push(base);
    }

    let parms = new URLSearchParams(window.location.search);
    let lang = i18nParamLang || parms.get('lang');
    if(lang) {
        add(lang);
    } else if(typeof navigator !== 'undefined' && navigator.languages) {
        for(let i = 0; i < navigator.languages.length; i++)
            add(navigator.languages[i]);
    }
    if(typeof navigator !== 'undefined' && navigator.language)
        add(navigator.language);
    add(i18nDefaultLocale);
    return candidates;
}

/**
 * i18nFetch fetches the catalog for the given locale.
 *
 * @param {string} locale
 * @returns {Promise<Object.<string,string>|null>}
 */
async function i18nFetch(locale) {
    try {
        let r = await fetch(`/locales/${locale}.json`);
        if(!r.ok)
            return null;
        let data = await r.json();
        if(data == null || typeof data !== 'object')
            return null;
        return data;
    } catch(e) {
        return null;
    }
}

async function initI18n() {
    let base = await i18nFetch(i18nDefaultLocale);
    if(base)
        i18nBase = base;

    let candidates = i18nCandidates();
    for(let i = 0; i < candidates.length; i++) {
        let locale = candidates[i];
        if(locale === i18nDefaultLocale) {
            i18nLocale = i18nDefaultLocale;
            i18nMessages = i18nBase;
            break;
        }
        let messages = await i18nFetch(locale);
        if(messages) {
            i18nLocale = locale;
            i18nMessages = messages;
            break;
        }
    }

    document.documentElement.lang = i18nLocale;
    translateDOM(document);
}

/**
 * i18nPlural returns the CLDR plural category for count in the active
 * locale.
 *
 * @param {number} count
 * @returns {string}
 */
function i18nPlural(count) {
    try {
        let IntlAny = /** @type {any} */ (Intl);
        return new IntlAny.PluralRules(i18nLocale).select(count);
    } catch(e) {
        return count === 1 ? 'one' : 'other';
    }
}

/**
 * t translates msgid, interpolating {{name}} placeholders from params.
 *
 * @param {string} msgid
 * @param {Object.<string,any>} [params]
 * @returns {string}
 */
function t(msgid, params) {
    /** @type {string|undefined} */
    let s;
    if(params && typeof params.count === 'number') {
        let cat = i18nPlural(params.count);
        s = i18nMessages[msgid + '_' + cat];
        if(s === undefined)
            s = i18nMessages[msgid + '_other'];
        if(s === undefined)
            s = i18nBase[msgid + '_' + cat];
        if(s === undefined)
            s = i18nBase[msgid + '_other'];
        if(s === undefined)
            s = i18nMessages[msgid];
        if(s === undefined)
            s = i18nBase[msgid];
    } else {
        s = i18nMessages[msgid];
        if(s === undefined)
            s = i18nBase[msgid];
    }
    if(s === undefined)
        s = msgid;

    if(params) {
        s = s.replace(/\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g,
                      function(m, name) {
                          return (name in params) ?
                              String(params[name]) : m;
                      });
    }
    return s;
}

/**
 * msg marks a string for extraction by tools/extract-i18n and returns it
 * unchanged. It is used for strings that are translated at a later point,
 * such as command descriptions defined before the catalog is loaded.
 *
 * @param {string} s
 * @returns {string}
 */
function msg(s) {
    return s;
}

/**
 * translateDOM translates the static parts of the document that are marked
 * with data-i18n (text content) and data-i18n-<attr> (attributes).
 *
 * @param {ParentNode} [root]
 */
function translateDOM(root) {
    if(!root)
        root = document;
    let elements = root.querySelectorAll('*');
    for(let i = 0; i < elements.length; i++) {
        let elt = /** @type {Element} */ (elements[i]);
        if(elt.hasAttribute('data-i18n')) {
            let key = elt.getAttribute('data-i18n');
            if(!key)
                key = elt.textContent || '';
            elt.textContent = t(key);
        }
        let names = elt.getAttributeNames();
        for(let j = 0; j < names.length; j++) {
            let attr = names[j];
            if(attr.indexOf('data-i18n-') !== 0)
                continue;
            let target = attr.slice('data-i18n-'.length);
            let key = elt.getAttribute(attr);
            if(!key)
                key = elt.getAttribute(target) || '';
            elt.setAttribute(target, t(key));
        }
    }
}

/**
 * i18nReady resolves once the translation catalog has been loaded and the
 * static DOM has been translated.
 *
 * @type {Promise<void>}
 */
const i18nReady = initI18n();
