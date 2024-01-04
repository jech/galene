// Copyright (c) 2021 by Juliusz Chroboczek.

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

async function listStats() {
    let table = document.getElementById('stats-table');

    let l;
    try {
        let r = await fetch('/galene-api/stats');
        if(!r.ok)
            throw new Error(`${r.status} ${r.statusText}`);
        l = await r.json();
    } catch(e) {
        console.error(e);
        table.textContent = `Couldn't fetch stats: ${e}`;
        return;
    }

    if(l.length === 0) {
        table.textContent = '(No group found.)';
        return;
    }

    for(let i = 0; i < l.length; i++)
        formatGroup(table, l[i]);
}

function formatGroup(table, group) {
    let tr = document.createElement('tr');
    let td = document.createElement('td');
    td.textContent = group.name;
    tr.appendChild(td);
    table.appendChild(tr);
    if(group.clients) {
        for(let i = 0; i < group.clients.length; i++) {
            let client = group.clients[i];
            let tr2 = document.createElement('tr');
            tr2.appendChild(document.createElement('td'));
            let td2 = document.createElement('td');
            td2.textContent = client.id;
            tr2.appendChild(td2);
            table.appendChild(tr2);
            if(client.up)
                for(let j = 0; j < client.up.length; j++) {
                    formatConn(table, '↑', client.up[j]);
                }
            if(client.down)
                for(let j = 0; j < client.down.length; j++) {
                    formatConn(table, '↓', client.down[j]);
                }
        }
    }
    return tr;
}

function formatConn(table, direction, conn) {
    let tr = document.createElement('tr');
    tr.appendChild(document.createElement('td'));
    tr.appendChild(document.createElement('td'));
    let td = document.createElement('td');
    td.textContent = conn.id;
    tr.appendChild(td);
    let td2 = document.createElement('td');
    td2.textContent = direction;
    tr.appendChild(td2);
    let td3 = document.createElement('td');
    if(conn.maxBitrate)
        td3.textContent = `${conn.maxBitrate}`;
    tr.appendChild(td3);
    table.appendChild(tr);
    if(conn.tracks) {
        for(let i = 0; i < conn.tracks.length; i++)
            formatTrack(table, conn.tracks[i]);
    }
}

function formatTrack(table, track) {
    let tr = document.createElement('tr');
    tr.appendChild(document.createElement('td'));
    tr.appendChild(document.createElement('td'));
    tr.appendChild(document.createElement('td'));
    let td = document.createElement('td');
    let layer = '';
    if(track.sid || track.maxSid)
        layer = layer + `s${track.sid}/${track.maxSid}`;
    if(track.tid || track.maxTid) {
        if(layer !== '')
            layer = layer + '+';
        layer = layer + `t${track.tid}/${track.maxTid}`;
    }
    td.textContent = layer;
    tr.appendChild(td);
    let td2 = document.createElement('td');
    if(track.maxBitrate)
        td2.textContent = `${track.bitrate||0}/${track.maxBitrate}`;
    else
        td2.textContent = `${track.bitrate||0}`;
    tr.appendChild(td2);
    let td3 = document.createElement('td');
    td3.textContent = `${Math.round(track.loss * 100)}%`;
    tr.appendChild(td3);
    let td4 = document.createElement('td');
    let text = '';
    if(track.rtt) {
        text = text + `${Math.round(track.rtt * 1000) / 1000}ms`;
    }
    if(track.jitter)
        text = text + `±${Math.round(track.jitter * 1000) / 1000}ms`;
    td4.textContent = text;
    tr.appendChild(td4);
    table.appendChild(tr);
}

listStats();
