// Copyright (c) 2019-2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

'use strict';

document.getElementById('groupform').onsubmit = function(e) {
    e.preventDefault();
    let group = document.getElementById('group').value.trim();

    location.href = '/group/' + group;
}

async function listPublicGroups() {
    let div = document.getElementById('public-groups');
    let table = document.getElementById('public-groups-table');

    let l;
    try {
        l = await (await fetch('/public-groups.json')).json();
    } catch(e) {
        console.error(e);
        l = [];
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
        a.textContent = group.name;
        a.href = '/group/' + encodeURIComponent(group.name);
        td.appendChild(a);
        tr.appendChild(td);
        let td2 = document.createElement('td');
        td2.textContent = `(${group.clientCount} clients)`;
        tr.appendChild(td2);
        table.appendChild(tr);
    }
}


listPublicGroups();
