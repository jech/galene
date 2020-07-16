// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

'use strict';

let myid;

let group;

let socket;

let up = {}, down = {};

let iceServers = [];

let permissions = {};

function toHex(array) {
    let a = new Uint8Array(array);
    function hex(x) {
        let h = x.toString(16);
        if(h.length < 2)
            h = '0' + h;
        return h;
    }
    return a.reduce((x, y) => x + hex(y), '');
}

function randomid() {
    let a = new Uint8Array(16);
    crypto.getRandomValues(a);
    return toHex(a);
}

function Connection(id, pc) {
    this.id = id;
    this.kind = null;
    this.label = null;
    this.pc = pc;
    this.stream = null;
    this.labels = {};
    this.labelsByMid = {};
    this.iceCandidates = [];
    this.timers = [];
    this.stats = {};
}

Connection.prototype.setInterval = function(f, t) {
    this.timers.push(setInterval(f, t));
};

Connection.prototype.close = function(sendit) {
    while(this.timers.length > 0)
        clearInterval(this.timers.pop());

    if(this.stream) {
        this.stream.getTracks().forEach(t => {
            try {
                t.stop();
            } catch(e) {
            }
        });
    }
    this.pc.close();

    if(sendit) {
        try {
            send({
                type: 'close',
                id: this.id,
            });
        } catch(e) {
        }
    }
};

function setUserPass(username, password) {
    window.sessionStorage.setItem(
        'userpass',
        JSON.stringify({username: username, password: password}),
    );
}

function getUserPass() {
    let userpass = window.sessionStorage.getItem('userpass');
    if(!userpass)
        return null;
    return JSON.parse(userpass);
}

function getUsername() {
    let userpass = getUserPass();
    if(!userpass)
        return null;
    return userpass.username;
}

function setConnected(connected) {
    let statspan = document.getElementById('statspan');
    let userform = document.getElementById('userform');
    let disconnectbutton = document.getElementById('disconnectbutton');
    if(connected) {
        clearError();
        statspan.textContent = 'Connected';
        statspan.classList.remove('disconnected');
        statspan.classList.add('connected');
        userform.classList.add('invisible');
        userform.classList.remove('userform');
        disconnectbutton.classList.remove('invisible');
        displayUsername();
    } else {
        let userpass = getUserPass();
        document.getElementById('username').value =
            userpass ? userpass.username : '';
        document.getElementById('password').value =
            userpass ? userpass.password : '';
        statspan.textContent = 'Disconnected';
        statspan.classList.remove('connected');
        statspan.classList.add('disconnected');
        userform.classList.add('userform');
        userform.classList.remove('invisible');
        disconnectbutton.classList.add('invisible');
        permissions={};
        clearUsername(false);
    }
}

document.getElementById('presentbutton').onclick = function(e) {
    e.preventDefault();
    addLocalMedia();
};

document.getElementById('unpresentbutton').onclick = function(e) {
    e.preventDefault();
    delUpMediaKind('local');
};

function changePresentation() {
    let id = findUpMedia('local');
    if(id) {
        addLocalMedia(id);
    }
}

function setVisibility(id, visible) {
    let elt = document.getElementById(id);
    if(visible)
        elt.classList.remove('invisible');
    else
        elt.classList.add('invisible');
}

function setButtonsVisibility() {
    let local = !!findUpMedia('local');
    let share = !!findUpMedia('screenshare')
    // don't allow multiple presentations
    setVisibility('presentbutton', permissions.present && !local);
    setVisibility('unpresentbutton', local);
    // allow multiple shared documents
    setVisibility('sharebutton', permissions.present);
    setVisibility('unsharebutton', share);

    setVisibility('mediaoptions', permissions.present);
}

let localMute = false;

function toggleLocalMute() {
    setLocalMute(!localMute);
}

function setLocalMute(mute) {
    localMute = mute;
    muteLocalTracks(localMute);
    let button = document.getElementById('mutebutton');
    button.textContent = localMute ? 'Unmute' : 'Mute';
    if(localMute)
        button.classList.add('muted');
    else
        button.classList.remove('muted');
}

document.getElementById('videoselect').onchange = function(e) {
    e.preventDefault();
    changePresentation();
};

document.getElementById('audioselect').onchange = function(e) {
    e.preventDefault();
    changePresentation();
};

document.getElementById('mutebutton').onclick = function(e) {
    e.preventDefault();
    toggleLocalMute();
}

document.getElementById('sharebutton').onclick = function(e) {
    e.preventDefault();
    addShareMedia();
};

document.getElementById('unsharebutton').onclick = function(e) {
    e.preventDefault();
    delUpMediaKind('screenshare');
}

document.getElementById('requestselect').onchange = function(e) {
    e.preventDefault();
    sendRequest(this.value);
};

async function updateStats(conn, sender) {
    let tid = sender.track && sender.track.id;
    if(!tid)
        return;

    let stats = conn.stats[tid];
    if(!stats) {
        conn.stats[tid] = {};
        stats = conn.stats[tid];
    }

    let report;
    try {
        report = await sender.getStats();
    } catch(e) {
        delete(stats[id].rate);
        delete(stats.timestamp);
        delete(stats.bytesSent);
        return;
    }

    for(let r of report.values()) {
        if(r.type !== 'outbound-rtp')
            continue;

        if(stats.timestamp) {
            stats.rate =
                ((r.bytesSent - stats.bytesSent) * 1000 /
                 (r.timestamp - stats.timestamp)) * 8;
        } else {
            delete(stats.rate);
        }
        stats.timestamp = r.timestamp;
        stats.bytesSent = r.bytesSent;
        return;
    }
}

function displayStats(id) {
    let conn = up[id];
    if(!conn) {
        setLabel(id);
        return;
    }

    let text = '';

    conn.pc.getSenders().forEach(s => {
        let tid = s.track && s.track.id;
        let stats = tid && conn.stats[tid];
        if(stats && stats.rate > 0) {
            if(text)
                text = text + ' + ';
            text = text + Math.round(stats.rate / 1000) + 'kbps';
        }
    });

    setLabel(id, text);
}

function mapMediaOption(value) {
    console.assert(typeof(value) === 'string');
    switch(value) {
    case 'default':
        return true;
    case 'off':
        return false;
    default:
        return {deviceId: value};
    }
}

function addSelectOption(select, label, value) {
    if(!value)
        value = label;
    for(let i = 0; i < select.children.length; i++) {
        if(select.children[i].value === value) {
            return;
        }
    }

    let option = document.createElement('option');
    option.value = value;
    option.textContent = label;
    select.appendChild(option);
}

// media names might not be available before we call getDisplayMedia.  So
// we call this lazily.
let mediaChoicesDone = false;

async function setMediaChoices() {
    if(mediaChoicesDone)
        return;

    let devices = [];
    try {
        devices = await navigator.mediaDevices.enumerateDevices();
    } catch(e) {
        console.error(e);
        return;
    }

    let cn = 1, mn = 1;

    devices.forEach(d => {
        let label = d.label;
        if(d.kind === 'videoinput') {
            if(!label)
                label = `Camera ${cn}`;
            addSelectOption(document.getElementById('videoselect'),
                            label, d.deviceId);
            cn++;
        } else if(d.kind === 'audioinput') {
            if(!label)
                label = `Microphone ${mn}`;
            addSelectOption(document.getElementById('audioselect'),
                            label, d.deviceId);
            mn++;
        }
    });

    mediaChoicesDone = true;
}

async function addLocalMedia(id) {
    if(!getUserPass())
        return;

    let audio = mapMediaOption(document.getElementById('audioselect').value);
    let video = mapMediaOption(document.getElementById('videoselect').value);

    if(!audio && !video) {
        if(id)
            delUpMedia(id);
        return;
    }

    if(id)
        stopUpMedia(id);

    let constraints = {audio: audio, video: video};
    let stream = null;
    try {
        stream = await navigator.mediaDevices.getUserMedia(constraints);
    } catch(e) {
        console.error(e);
        if(id)
            delUpMedia(id);
        return;
    }

    setMediaChoices();

    id = await newUpStream(id);
    let c = up[id];

    c.kind = 'local';
    c.stream = stream;
    stream.getTracks().forEach(t => {
        c.labels[t.id] = t.kind
        if(t.kind == 'audio' && localMute)
            t.enabled = false;
        let sender = c.pc.addTrack(t, stream);
        c.setInterval(() => {
            updateStats(c, sender);
        }, 2000);
    });
    c.setInterval(() => {
        displayStats(id);
    }, 2500);
    await setMedia(id);
    setButtonsVisibility()
}

async function addShareMedia(setup) {
    if(!getUserPass())
        return;

    let stream = null;
    try {
        stream = await navigator.mediaDevices.getDisplayMedia({});
    } catch(e) {
        console.error(e);
        return;
    }

    let id = await newUpStream();
    let c = up[id];
    c.kind = 'screenshare';
    c.stream = stream;
    stream.getTracks().forEach(t => {
        let sender = c.pc.addTrack(t, stream);
        t.onended = e => {
            delUpMedia(id);
        };
        c.labels[t.id] = 'screenshare';
        c.setInterval(() => {
            updateStats(c, sender);
        }, 2000);
    });
    c.setInterval(() => {
        displayStats(id);
    }, 2500);
    await setMedia(id);
    setButtonsVisibility()
}

function stopUpMedia(id) {
    let c = up[id];
    if(!c) {
        console.error('Stopping unknown up media');
        return;
    }
    if(!c.stream)
        return;
    c.stream.getTracks().forEach(t => {
        try {
            t.stop();
        } catch(e) {
        }
    });
}

function delUpMedia(id) {
    let c = up[id];
    if(!c) {
        console.error('Deleting unknown up media');
        return;
    }
    stopUpMedia(id);
    delMedia(id);
    c.close(true);
    delete(up[id]);
    setButtonsVisibility()
}

function delUpMediaKind(kind) {
    for(let id in up) {
        let c = up[id];
        if(c.kind != kind)
            continue
        c.close(true);
        delMedia(id);
        delete(up[id]);
    }

    setButtonsVisibility()
}

function findUpMedia(kind) {
    for(let id in up) {
        if(up[id].kind === kind)
            return id;
    }
    return null;
}

function muteLocalTracks(mute) {
    for(let id in up) {
        let c = up[id];
        if(c.kind === 'local') {
            let stream = c.stream;
            stream.getTracks().forEach(t => {
                if(t.kind === 'audio') {
                    t.enabled = !mute;
                }
            });
        }
    }
}

function setMedia(id) {
    let mine = true;
    let c = up[id];
    if(!c) {
        c = down[id];
        mine = false;
    }
    if(!c)
        throw new Error('Unknown connection');

    let peersdiv = document.getElementById('peers');

    let div = document.getElementById('peer-' + id);
    if(!div) {
        div = document.createElement('div');
        div.id = 'peer-' + id;
        div.classList.add('peer');
        peersdiv.appendChild(div);
    }

    let media = document.getElementById('media-' + id);
    if(!media) {
        media = document.createElement('video');
        media.id = 'media-' + id;
        media.classList.add('media');
        media.autoplay = true;
        media.playsinline = true;
        media.controls = true;
        if(mine)
            media.muted = true;
        div.appendChild(media);
    }

    let label = document.getElementById('label-' + id);
    if(!label) {
        label = document.createElement('div');
        label.id = 'label-' + id;
        label.classList.add('label');
        div.appendChild(label);
    }

    media.srcObject = c.stream;
    setLabel(id);
    setMediaStatus(id);

    resizePeers();
}

function delMedia(id) {
    let mediadiv = document.getElementById('peers');
    let peer = document.getElementById('peer-' + id);
    let media = document.getElementById('media-' + id);

    media.srcObject = null;
    mediadiv.removeChild(peer);

    resizePeers();
}

function setMediaStatus(id) {
    let c = up[id] || down[id];
    let state = c && c.pc && c.pc.iceConnectionState;
    let good = state === 'connected' || state === 'completed';

    let media = document.getElementById('media-' + id);
    if(!media) {
        console.warn('Setting status of unknown media.');
        return;
    }
    if(good)
        media.classList.remove('media-failed');
    else
        media.classList.add('media-failed');
}


function setLabel(id, fallback) {
    let label = document.getElementById('label-' + id);
    if(!label)
        return;
    let l = down[id] ? down[id].label : null;
    if(l) {
        label.textContent = l;
        label.classList.remove('label-fallback');
    } else if(fallback) {
        label.textContent = fallback;
        label.classList.add('label-fallback');
    } else {
        label.textContent = '';
        label.classList.remove('label-fallback');
    }
}

function resizePeers() {
    let count = Object.keys(up).length + Object.keys(down).length;
    let columns = Math.ceil(Math.sqrt(count));
    document.getElementById('peers').style['grid-template-columns'] =
        `repeat(${columns}, 1fr)`;
}

function serverConnect() {
    if(socket) {
        socket.close(1000, 'Reconnecting');
        socket = null;
        setConnected(false);
    }

    try {
        socket = new WebSocket(
            `ws${location.protocol === 'https:' ? 's' : ''}://${location.host}/ws`,
        );
    } catch(e) {
        console.error(e);
        setConnected(false);
        return Promise.reject(e);
    }

    return new Promise((resolve, reject) => {
        socket.onerror = function(e) {
            reject(e.error ? e.error : e);
        };
        socket.onopen = function(e) {
            resetUsers();
            resetChat();
            setConnected(true);
            let up = getUserPass();
            try {
                send({
                    type: 'handshake',
                    id: myid,
                    group: group,
                    username: up.username,
                    password: up.password,
                })
                sendRequest(document.getElementById('requestselect').value);
                } catch(e) {
                    console.error(e);
                    displayError(e);
                    reject(e);
                    return;
                }
            resolve();
        };
        socket.onclose = function(e) {
            setConnected(false);
            delUpMediaKind('local');
            delUpMediaKind('screenshare');
            for(let id in down) {
                let c = down[id];
                delete(down[id]);
                c.close(false);
                delMedia(id);
            }
            reject(new Error('websocket close ' + e.code + ' ' + e.reason));
        };
        socket.onmessage = function(e) {
            let m = JSON.parse(e.data);
            switch(m.type) {
            case 'offer':
                gotOffer(m.id, m.labels, m.offer, !!m.renegotiate);
                break;
            case 'answer':
                gotAnswer(m.id, m.answer);
                break;
            case 'close':
                gotClose(m.id);
                break;
            case 'abort':
                gotAbort(m.id);
                break;
            case 'ice':
                gotICE(m.id, m.candidate);
                break;
            case 'label':
                gotLabel(m.id, m.value);
                break;
            case 'permissions':
                gotPermissions(m.permissions);
                break;
            case 'user':
                gotUser(m.id, m.username, m.del);
                break;
            case 'chat':
                addToChatbox(m.id, m.username, m.value, m.me);
                break;
            case 'clearchat':
                resetChat();
                break;
            case 'ping':
                send({
                    type: 'pong',
                });
                break;
            case 'pong':
                /* nothing */
                break;
            case 'error':
                displayError('The server said: ' + m.value);
                break;
            default:
                console.warn('Unexpected server message', m.type);
                return;
            }
        };
    });
}

function sendRequest(value) {
    let request = [];
    switch(value) {
    case 'audio':
        request = {audio: true};
        break;
    case 'screenshare':
        request = {audio: true, screenshare: true};
        break;
    case 'everything':
        request = {audio: true, screenshare: true, video: true};
        break;
    default:
        console.error(`Uknown value ${value} in sendRequest`);
        break;
    }

    send({
        type: 'request',
        request: request,
    });
}

async function gotOffer(id, labels, offer, renegotiate) {
    let c = down[id];
    if(c && !renegotiate) {
        // SDP is rather inflexible as to what can be renegotiated.
        // Unless the server indicates that this is a renegotiation with
        // all parameters unchanged, tear down the existing connection.
        delete(down[id])
        c.close(false);
        c = null;
    }

    if(!c) {
        let pc = new RTCPeerConnection({
            iceServers: iceServers,
        });
        c = new Connection(id, pc);
        down[id] = c;

        c.pc.onicecandidate = function(e) {
            if(!e.candidate)
                return;
            send({type: 'ice',
                  id: id,
                  candidate: e.candidate,
                 });
        };

        pc.oniceconnectionstatechange = e => {
            setMediaStatus(id);
        }

        c.pc.ontrack = function(e) {
            let label = e.transceiver && c.labelsByMid[e.transceiver.mid];
            if(label) {
                c.labels[e.track.id] = label;
            } else {
                console.warn("Couldn't find label for track");
            }
            c.stream = e.streams[0];
            setMedia(id);
        };
    }

    c.labelsByMid = labels;

    await c.pc.setRemoteDescription(offer);
    await addIceCandidates(c);
    let answer = await c.pc.createAnswer();
    if(!answer)
        throw new Error("Didn't create answer");
    await c.pc.setLocalDescription(answer);
    send({
        type: 'answer',
        id: id,
        answer: answer,
    });
}

function gotLabel(id, label) {
    let c = down[id];
    if(!c)
        throw new Error('Got label for unknown id');

    c.label = label;
    setLabel(id);
}

async function gotAnswer(id, answer) {
    let c = up[id];
    if(!c)
        throw new Error('unknown up stream');
    try {
        await c.pc.setRemoteDescription(answer);
    } catch(e) {
        console.error(e);
        displayError(e);
        delUpMedia(id);
        return;
    }
    await addIceCandidates(c);
}

function gotClose(id) {
    let c = down[id];
    if(!c)
        throw new Error('unknown down stream');
    delete(down[id]);
    c.close(false);
    delMedia(id);
}

function gotAbort(id) {
    delUpMedia(id);
}

async function gotICE(id, candidate) {
    let conn = up[id];
    if(!conn)
        conn = down[id];
    if(!conn)
        throw new Error('unknown stream');
    if(conn.pc.remoteDescription)
        await conn.pc.addIceCandidate(candidate).catch(console.warn);
    else
        conn.iceCandidates.push(candidate);
}

async function addIceCandidates(conn) {
    let promises = [];
    conn.iceCandidates.forEach(c => {
        promises.push(conn.pc.addIceCandidate(c).catch(console.warn));
    });
    conn.iceCandidates = [];
    return await Promise.all(promises);
}

function send(m) {
    if(!m)
        throw(new Error('Sending null message'));
    if(socket.readyState !== socket.OPEN) {
        // send on a closed connection doesn't throw
        throw(new Error('Connection is not open'));
    }
    return socket.send(JSON.stringify(m));
}

let users = {};

function addUser(id, name) {
    if(!name)
        name = null;
    if(id in users)
        throw new Error('Duplicate user id');
    users[id] = name;

    let div = document.getElementById('users');
    let user = document.createElement('div');
    user.id = 'user-' + id;
    user.textContent = name ? name : '(anon)';
    div.appendChild(user);
}

function delUser(id, name) {
    if(!name)
        name = null;
    if(!(id in users))
        throw new Error('Unknown user id');
    if(users[id] !== name)
        throw new Error('Inconsistent user name');
    delete(users[id]);
    let div = document.getElementById('users');
    let user = document.getElementById('user-' + id);
    div.removeChild(user);
}

function resetUsers() {
    for(let id in users)
        delUser(id, users[id]);
}

function gotUser(id, name, del) {
    if(del)
        delUser(id, name);
    else
        addUser(id, name);
}

function displayUsername() {
    let userpass = getUserPass();
    let text = '';
    if(userpass && userpass.username)
        text = 'as ' + userpass.username;
    if(permissions.op && permissions.present)
        text = text + ' (op, presenter)';
    else if(permissions.op)
        text = text + ' (op)';
    else if(permissions.present)
        text = text + ' (presenter)';
    document.getElementById('userspan').textContent = text;
}

function clearUsername() {
    document.getElementById('userspan').textContent = '';
}

function gotPermissions(perm) {
    permissions = perm;
    displayUsername();
    setButtonsVisibility();
}

const urlRegexp = /https?:\/\/[-a-zA-Z0-9@:%/._\\+~#=?]+[-a-zA-Z0-9@:%/_\\+~#=]/g;

function formatLine(line) {
    let r = new RegExp(urlRegexp);
    let result = [];
    let pos = 0;
    while(true) {
        let m = r.exec(line);
        if(!m)
            break;
        result.push(document.createTextNode(line.slice(pos, m.index)));
        let a = document.createElement('a');
        a.href = m[0];
        a.textContent = m[0];
        a.target = '_blank';
        a.rel = 'noreferrer noopener';
        result.push(a);
        pos = m.index + m[0].length;
    }
    result.push(document.createTextNode(line.slice(pos)));
    return result;
}

function formatLines(lines) {
    let elts = [];
    if(lines.length > 0)
        elts = formatLine(lines[0]);
    for(let i = 1; i < lines.length; i++) {
        elts.push(document.createElement('br'));
        elts = elts.concat(formatLine(lines[i]));
    }
    let elt = document.createElement('p');
    elts.forEach(e => elt.appendChild(e));
    return elt;
}

let lastMessage = {};

function addToChatbox(peerId, nick, message, me){
    let container = document.createElement('div');
    container.classList.add('message');
    if(!me) {
        let p = formatLines(message.split('\n'));
        if (lastMessage.nick !== nick || lastMessage.peerId !== peerId) {
            let user = document.createElement('p');
            user.textContent = nick;
            user.classList.add('message-user');
            container.appendChild(user);
        }
        p.classList.add('message-content');
        container.appendChild(p);
        lastMessage.nick = nick;
        lastMessage.peerId = peerId;
    } else {
        let asterisk = document.createElement('span');
        asterisk.textContent = '*';
        asterisk.classList.add('message-me-asterisk');
        let user = document.createElement('span');
        user.textContent = nick;
        user.classList.add('message-me-user');
        let content = document.createElement('span');
        formatLine(message).forEach(elt => {
            content.appendChild(elt);
        });
        content.classList.add('message-me-content');
        container.appendChild(asterisk);
        container.appendChild(user);
        container.appendChild(content);
        container.classList.add('message-me');
        delete(lastMessage.nick);
        delete(lastMessage.peerId);
    }

    let box = document.getElementById('box');
    box.appendChild(container);
    if(box.scrollHeight > box.clientHeight) {
        box.scrollTop = box.scrollHeight - box.clientHeight;
    }

    return message;
}

function resetChat() {
    lastMessage = {};
    document.getElementById('box').textContent = '';
}

function handleInput() {
    let username = getUsername();
    let input = document.getElementById('input');
    let data = input.value;
    input.value = '';

    let message, me;

    if(data === '')
        return;

    if(data.charAt(0) === '/') {
        if(data.charAt(1) === '/') {
            message = data.substring(1);
            me = false;
        } else {
            let space, cmd, rest;
            space = data.indexOf(' ');
            if(space < 0) {
                cmd = data;
                rest = '';
            } else {
                cmd = data.slice(0, space);
                rest = data.slice(space + 1).trim();
            }

            switch(cmd) {
            case '/me':
                message = rest;
                me = true;
                break;
            case '/leave':
                socket.close();
                return;
            case '/clear':
                if(!permissions.op) {
                    displayError("You're not an operator");
                    return;
                }
                send({
                    type: 'clearchat',
                });
                return;
            case '/lock':
            case '/unlock':
                if(!permissions.op) {
                    displayError("You're not an operator");
                    return;
                }
                send({
                    type: cmd.slice(1),
                });
                return;
            case '/record':
            case '/unrecord':
                if(!permissions.record) {
                    displayError("You're not allowed to record");
                    return;
                }
                send({
                    type: cmd.slice(1),
                });
                return;
            case '/op':
            case '/unop':
            case '/kick':
            case '/present':
            case '/unpresent': {
                if(!permissions.op) {
                    displayError("You're not an operator");
                    return;
                }
                let id;
                if(id in users) {
                    id = rest;
                } else {
                    for(let i in users) {
                        if(users[i] === rest) {
                            id = i;
                            break;
                        }
                    }
                }
                if(!id) {
                    displayError('Unknown user ' + rest);
                    return;
                }
                send({
                    type: cmd.slice(1),
                    id: id,
                });
                return;
            }
            default:
                displayError('Uknown command ' + cmd);
                return;
            }
        }
    } else {
        message = data;
        me = false;
    }

    if(!username) {
        displayError("Sorry, you're anonymous, you cannot chat");
        return;
    }

    try {
        let a = send({
            type: 'chat',
            id: myid,
            username: username,
            value: message,
            me: me,
        });
        addToChatbox(myid, username, message, me);
    } catch(e) {
        console.error(e);
        displayError(e);
    }
}

document.getElementById('inputform').onsubmit = function(e) {
    e.preventDefault();
    handleInput();
};

document.getElementById('input').onkeypress = function(e) {
    if(e.key === 'Enter' && !e.ctrlKey && !e.shiftKey && !e.metaKey) {
        e.preventDefault();
        handleInput();
    }
};

function chatResizer(e) {
    e.preventDefault();
    let chat = document.getElementById('chat');
    let start_x = e.clientX;
    let start_width = parseFloat(
        document.defaultView.getComputedStyle(chat).width.replace('px', ''),
    );
    let inputbutton = document.getElementById('inputbutton');
    function start_drag(e) {
        let width = start_width + e.clientX - start_x;
        if(width < 40)
            inputbutton.style.display = 'none';
        else
            inputbutton.style.display = 'inline';
        chat.style.width = width + 'px';
    }
    function stop_drag(e) {
        document.documentElement.removeEventListener(
            'mousemove', start_drag, false,
        );
        document.documentElement.removeEventListener(
            'mouseup', stop_drag, false,
        );
    }

    document.documentElement.addEventListener(
        'mousemove', start_drag, false,
    );
    document.documentElement.addEventListener(
        'mouseup', stop_drag, false,
    );
}

document.getElementById('resizer').addEventListener('mousedown', chatResizer, false);

async function newUpStream(id) {
    if(!id) {
        id = randomid();
        if(up[id])
            throw new Error('Eek!');
    }
    let pc = new RTCPeerConnection({
        iceServers: iceServers,
    });
    if(!pc)
        throw new Error("Couldn't create peer connection");
    if(up[id]) {
        up[id].close(false);
    }
    up[id] = new Connection(id, pc);

    pc.onnegotiationneeded = async e => {
        try {
            await negotiate(id);
        } catch(e) {
            console.error(e);
            displayError(e);
            delUpMedia(id);
        }
    }

    pc.onicecandidate = e => {
        if(!e.candidate)
            return;
        send({type: 'ice',
             id: id,
             candidate: e.candidate,
             });
    };

    pc.oniceconnectionstatechange = e => {
        setMediaStatus(id);
        if(pc.iceConnectionState === 'failed') {
            try {
                pc.restartIce();
            } catch(e) {
                console.error(e);
                displayError(e);
            }
        }
    }

    pc.ontrack = console.error;

    return id;
}

async function negotiate(id) {
    let c = up[id];
    if(!c)
        throw new Error('unknown connection');

    if(typeof(c.pc.getTransceivers) !== 'function')
        throw new Error('Browser too old, please upgrade');

    let offer = await c.pc.createOffer({});
    if(!offer)
        throw(new Error("Didn't create offer"));
    await c.pc.setLocalDescription(offer);

    // mids are not known until this point
    c.pc.getTransceivers().forEach(t => {
        if(t.sender && t.sender.track) {
            let label = c.labels[t.sender.track.id];
            if(label)
                c.labelsByMid[t.mid] = label;
            else
                console.warn("Couldn't find label for track");
        }
    });

    send({
        type: 'offer',
        id: id,
        labels: c.labelsByMid,
        offer: offer,
    });
}

let errorTimeout = null;

function setErrorTimeout(ms) {
    if(errorTimeout) {
        clearTimeout(errorTimeout);
        errorTimeout = null;
    }
    if(ms) {
        errorTimeout = setTimeout(clearError, ms);
    }
}

function displayError(message) {
    let errspan = document.getElementById('errspan');
    errspan.textContent = message;
    errspan.classList.remove('noerror');
    errspan.classList.add('error');
    setErrorTimeout(8000);
}

function displayWarning(message) {
    // don't overwrite real errors
    if(!errorTimeout)
        return displayError(message);
}

function clearError() {
    let errspan = document.getElementById('errspan');
    errspan.textContent = '';
    errspan.classList.remove('error');
    errspan.classList.add('noerror');
    setErrorTimeout(null);
}

async function getIceServers() {
    let r = await fetch('/ice-servers.json');
    if(!r.ok)
        throw new Error("Couldn't fetch ICE servers: " +
                        r.status + ' ' + r.statusText);
    let servers = await r.json();
    if(!(servers instanceof Array))
        throw new Error("couldn't parse ICE servers");
    iceServers = servers;
}

document.getElementById('userform').onsubmit = async function(e) {
    e.preventDefault();
    let username = document.getElementById('username').value.trim();
    let password = document.getElementById('password').value;
    setUserPass(username, password);
    await serverConnect();
};

document.getElementById('disconnectbutton').onclick = function(e) {
    socket.close();
};

function start() {
    group = decodeURIComponent(location.pathname.replace(/^\/[a-z]*\//, ''));
    let title = group.charAt(0).toUpperCase() + group.slice(1);
    if(group !== '') {
        document.title = title;
        document.getElementById('title').textContent = title;
    }

    setLocalMute(localMute);

    myid = randomid();

    getIceServers().catch(console.error).then(c => {
        document.getElementById('connectbutton').disabled = false;
    }).then(c => {
        let userpass = getUserPass();
        if(userpass)
            return serverConnect();
    });
}

start();
