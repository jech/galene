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
        l = await (await fetch('/stats.json')).json();
    } catch(e) {
        console.error(e);
        l = [];
    }

    if(l.length === 0) {
        table.textContent = '(No group found.)';
        return;
    }

    for(let i = 0; i < l.length; i++)
        table.appendChild(formatGroup(l[i]));
}

function formatGroup(group) {
    let tr = document.createElement('tr');
    let td = document.createElement('td');
    td.textContent = group.name;
    tr.appendChild(td);
    if(group.clients) {
        let td2 = document.createElement('td');
        let table = document.createElement('table');
        for(let i = 0; i < group.clients.length; i++) {
            let client = group.clients[i];
            let tr2 = document.createElement('tr');
            let td3 = document.createElement('td');
            td3.textContent = client.id;
            tr2.appendChild(td3);
            table.appendChild(tr2);
            if(client.up)
                for(let j = 0; j < client.up.length; j++)
                    table.appendChild(formatConn('↑', client.up[j]));
            if(client.down)
                for(let j = 0; j < client.down.length; j++)
                    table.appendChild(formatConn('↓', client.down[j]));
        }
        td2.appendChild(table);
        tr.appendChild(td2);
    }
    return tr;
}

function formatConn(direction, conn) {
    let tr = document.createElement('tr');
    let td = document.createElement('td');
    tr.appendChild(td);
    let td2 = document.createElement('td');
    td2.textContent = conn.id;
    tr.appendChild(td2);
    let td3 = document.createElement('td');
    if(conn.maxBitrate)
        td3.textContent = direction + ' ' + conn.maxBitrate;
    else
        td3.textContent = direction;
    tr.appendChild(td3);
    let td4 = document.createElement('td');
    if(conn.tracks) {
        let table = document.createElement('table');
        for(let i = 0; i < conn.tracks.length; i++)
            table.appendChild(formatTrack(conn.tracks[i]));
        td4.appendChild(table);
    }
    tr.appendChild(td4);
    return tr;
}

function formatTrack(track) {
    let tr = document.createElement('tr');
    let td = document.createElement('td');
    if(track.maxBitrate)
        td.textContent = `${track.bitrate||0}/${track.maxBitrate}`;
    else
        td.textContent = `${track.bitrate||0}`;
    tr.appendChild(td);
    let td2 = document.createElement('td');
    td2.textContent = `${Math.round(track.loss * 100)}%`;
    tr.appendChild(td2);
    let td3 = document.createElement('td');
    let text = '';
    if(track.rtt) {
        text = text + `${Math.round(track.rtt * 1000) / 1000}ms`;
    }
    if(track.jitter)
        text = text + `±${Math.round(track.jitter * 1000) / 1000}ms`;
    td3.textContent = text;
    tr.appendChild(td3);
    return tr;
}

listStats();
