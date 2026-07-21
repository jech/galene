// Copyright (c) 2020-2026 by Juliusz Chroboczek.

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
 * @typedef {Object} settings - the type of stored settings
 * @property {boolean} [localMute]
 * @property {string} [video]
 * @property {string} [audio]
 * @property {string} [simulcast]
 * @property {string} [send]
 * @property {string} [request]
 * @property {boolean} [activityDetection]
 * @property {boolean} [displayAll]
 * @property {Array.<number>} [resolution]
 * @property {boolean} [mirrorView]
 * @property {boolean} [blackboardMode]
 * @property {string} [filter]
 * @property {boolean} [preprocessing]
 * @property {boolean} [hqaudio]
 * @property {boolean} [forceRelay]
 */

/**
 * fallbackSettings is used to store settings if session storage is not
 * available.
 *
 * @type{settings}
 */
let fallbackSettings = null;

/**
 * Overwrite settings with the parameter.  This uses session storage if
 * available, and the global variable fallbackSettings otherwise.
 *
 * @param {settings} settings
 */
function storeSettings(settings) {
    try {
        window.sessionStorage.setItem('settings', JSON.stringify(settings));
        fallbackSettings = null;
    } catch(e) {
        console.warn("Couldn't store settings:", e);
        fallbackSettings = settings;
    }
}

/**
 * Return the current value of stored settings.  This always returns
 * a dictionary, even when there are no stored settings.
 *
 * @returns {settings}
 */
function getSettings() {
    /** @type {settings} */
    let settings;
    try {
        let json = window.sessionStorage.getItem('settings');
        settings = JSON.parse(json);
    } catch(e) {
        console.warn("Couldn't retrieve settings:", e);
        settings = fallbackSettings;
    }
    return settings || {};
}

/**
 * Update stored settings with the key/value pairs stored in the parameter.
 *
 * @param {settings} settings
 */
function updateSettings(settings) {
    let s = getSettings();
    for(let key in settings)
        s[key] = settings[key];
    storeSettings(s);
}

/**
 * Update a single key/value pair in the stored settings.
 *
 * @param {string} key
 * @param {any} value
 */
function updateSetting(key, value) {
    let s = {};
    s[key] = value;
    updateSettings(s);
}

/**
 * Remove a single key/value pair from the stored settings.
 *
 * @param {string} key
 */
function delSetting(key) {
    let s = getSettings();
    if(!(key in s))
        return;
    delete(s[key]);
    storeSettings(s);
}
