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

document.getElementById('groupform').onsubmit = function(e) {
    e.preventDefault();
    let group = document.getElementById('group').value.trim();
    if(group !== '')
        location.href = '/group/' + group + '/';
};

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
        a.href = '/group/' + group.name + '/';
        td.appendChild(a);
        tr.appendChild(td);
        let td2 = document.createElement('td');
        if(group.description)
            td2.textContent = group.description;
        tr.appendChild(td2);
        let td3 = document.createElement('td');
        let locked = group.locked ? ', locked' : '';
        td3.textContent = `(${group.clientCount} clients${locked})`;
        tr.appendChild(td3);
        table.appendChild(tr);
    }
}


listPublicGroups();
