// Copyright (c) 2020 by Juliusz Chroboczek.

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

document.getElementById('groupform').onsubmit = async function(e) {
    e.preventDefault();
    clearError();
    let groupinput = document.getElementById('group')
    let button = document.getElementById('submitbutton');

    let group = groupinput.value.trim();
    if(group === '')
        return;
    let url = '/group/' + group + '/';
    let statusUrl = url + '.status.json';

    try {
        groupinput.disabled = true;
        button.disabled = true;
        try {
            let resp = await fetch(statusUrl, {
                method: 'HEAD',
            });
            if(!resp.ok) {
                if(resp.status === 404)
                    displayError('No such group');
                else
                    displayError(`The server said: ${resp.status} ${resp.statusText}`);
                return;
            }
        } catch(e) {
            displayError(`Couldn't connect: ${e.toString()}`);
            return;
        }
    } finally {
        groupinput.disabled = false;
        button.disabled = false;
    }

    location.href = url;
};

var clearErrorTimeout = null;

function displayError(message) {
    clearError();
    let p = document.getElementById('errormessage');
    p.textContent = message;
    clearErrorTimeout = setTimeout(() => {
        let p = document.getElementById('errormessage');
        p.textContent = '';
        clearErrorTimeout = null;
    }, 2500);
}

function clearError() {
    if(clearErrorTimeout != null) {
        clearTimeout(clearErrorTimeout);
        clearErrorTimeout = null;
    }
}

async function listPublicGroups() {
    let div = document.getElementById('public-groups');
    let table = document.getElementById('public-groups-table');

    let l;
    try {
        let r = await fetch('/public-groups.json');
        if(!r.ok)
            throw new Error(`${r.status} ${r.statusText}`);
        l = await r.json();
    } catch(e) {
        table.textContent = `Couldn't fetch groups: ${e}`;
        div.classList.remove('nogroups');
        div.classList.add('groups');
        return;
    }

    if (l.length === 0) {
        table.textContent = '(No groups found.)';
        div.classList.remove('groups');
        div.classList.add('nogroups');
        return;
    }

    div.classList.remove('nogroups');
    div.classList.add('groups');

    for(let i = 0; i < l.length; i++) {
        let group = l[i];
        let tr = document.createElement('tr');
        let td = document.createElement('td');
        let a = document.createElement('a');
        a.textContent = group.displayName || group.name;
        a.href = group.location;
        td.appendChild(a);
        tr.appendChild(td);
        let td2 = document.createElement('td');
        if(group.description)
            td2.textContent = group.description;
        tr.appendChild(td2);
        let td3 = document.createElement('td');
        if(!group.redirect) {
            let locked = group.locked ? ', locked' : '';
            td3.textContent = `(${group.clientCount} clients${locked})`;
        } else {
            td3.textContent = '(remote)';
        }
        tr.appendChild(td3);
        table.appendChild(tr);
    }
}


listPublicGroups();
