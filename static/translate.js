// Copyright (c) 2023 by Laurent GAY

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

if (!String.prototype.format) {
	String.prototype.format = function() {
		var new_text = this;
		for (var key in arguments) {
			new_text = new_text.replace("{" + key + "}", arguments[key])
		}
		return new_text
	}
}

var language_dictionnary = null;

async function load_dictionnary() {
    let config;
    try {
        let r = await fetch('/guiconfig.json');
        if(!r.ok) {
            r = await fetch('/guiconfig-template.json');
            if(!r.ok)
                throw new Error(`${r.status} ${r.statusText}`);
        }
        config = await r.json();
    } catch(e) {
        console.error(e);
        config = {};
    }
	if (config.lang !== undefined) {
		try {
			let rep_lang = await fetch('/lang/'+ config.lang +'.json');
			if(rep_lang.ok) 
				language_dictionnary = await rep_lang.json();
		} catch(e) {
			console.error(e);
		}
	}
}

function translate_text(text) {
	if ((language_dictionnary !== null) && (text in language_dictionnary)) {
		return language_dictionnary[text];
	}
	console.debug('text "{0}" no found'.format(text));
	return text;
}

async function translate_document() {
	if (language_dictionnary !== null) {
		let element_to_translate = document.getElementsByClassName('lang');
		for(var item_index = 0; item_index < element_to_translate.length; item_index++) {
			var item = element_to_translate[item_index];
			if ((item.innerText==="") && item.hasAttribute("value")) {
				item.setAttribute("value", translate_text(item.getAttribute("value")));
			} else {
			  	item.innerText = translate_text(item.innerText);
			}
		}
	}
}

async function run_translate() {
	await load_dictionnary();
	await translate_document();
}
