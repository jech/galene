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
    let s = '';
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
    this.label = null;
    this.pc = pc;
    this.stream = null;
    this.iceCandidates = [];
}

Connection.prototype.close = function() {
    this.pc.close();
    send({
        type: 'close',
        id: this.id,
    });
}

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
    let diconnect = document.getElementById('disconnectbutton');
    if(connected) {
        statspan.textContent = 'Connected';
        statspan.classList.remove('disconnected');
        statspan.classList.add('connected');
        userform.classList.add('userform-invisible');
        userform.classList.remove('userform');
        disconnectbutton.classList.remove('disconnect-invisible');
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
        userform.classList.remove('userform-invisible');
        disconnectbutton.classList.add('disconnect-invisible');
        permissions={};
        clearUsername(false);
    }
}

document.getElementById('presenterbox').onchange = function(e) {
    e.preventDefault();
    setLocalMedia();
}

document.getElementById('sharebox').onchange = function(e) {
    e.preventDefault();
    setShareMedia();
}

let localMediaId = null;

async function setLocalMedia() {
    if(!getUserPass())
        return;

    if(!document.getElementById('presenterbox').checked) {
        if(localMediaId) {
            up[localMediaId].close();
            delete(up[localMediaId]);
            delMedia(localMediaId)
            localMediaId = null;
        }
        return;
    }

    if(!localMediaId) {
        let constraints = {audio: true, video: true};
        let opts = {video: true, audio: true};
        let stream = null;
        try {
            stream = await navigator.mediaDevices.getUserMedia(constraints);
        } catch(e) {
            console.error(e);
            return;
        }
        localMediaId = await newUpStream();

        let c = up[localMediaId];
        c.stream = stream;
        stream.getTracks().forEach(t => {
            c.pc.addTrack(t, stream);
        });
        await setMedia(localMediaId);
    }
}

let shareMediaId = null;

async function setShareMedia() {
    if(!getUserPass())
        return;

    if(!document.getElementById('sharebox').checked) {
        if(shareMediaId) {
            up[shareMediaId].close();
            delete(up[shareMediaId]);
            delMedia(shareMediaId)
            shareMediaId = null;
        }
        return;
    }
    if(!shareMediaId) {
        let constraints = {audio: true, video: true};
        let opts = {video: true, audio: true};
        let stream = null;
        try {
            stream = await navigator.mediaDevices.getDisplayMedia({});
        } catch(e) {
            console.error(e);
            return;
        }
        shareMediaId = await newUpStream();

        let c = up[shareMediaId];
        c.stream = stream;
        stream.getTracks().forEach(t => {
            c.pc.addTrack(t, stream);
            t.onended = e => {
                document.getElementById('sharebox').checked = false;
                setShareMedia();
            }
        });
        await setMedia(shareMediaId);
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
        div.appendChild(label)
    }

    media.srcObject = c.stream;
    setLabel(id);
}

function delMedia(id) {
    let mediadiv = document.getElementById('peers');
    let peer = document.getElementById('peer-' + id);
    let media = document.getElementById('media-' + id);

    media.srcObject = null;
    mediadiv.removeChild(peer);
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
            console.error(e);
            reject(e.error ? e.error : e);
        };
        socket.onopen = function(e) {
            resetUsers();
            setConnected(true);
            let up = getUserPass();
            send({
                type: 'handshake',
                id: myid,
                group: group,
                username: up.username,
                password: up.password,
            });
            resolve();
        };
        socket.onclose = function(e) {
            setConnected(false);
            document.getElementById('presenterbox').checked = false;
            document.getElementById('presenterbox').disabled = true;
            setLocalMedia();
            document.getElementById('sharebox').checked = false;
            document.getElementById('sharebox').disabled = true;
            setShareMedia();
            reject(new Error('websocket close ' + e.code + ' ' + e.reason));
        };
        socket.onmessage = function(e) {
            let m = JSON.parse(e.data);
            switch(m.type) {
            case 'offer':
                gotOffer(m.id, m.offer);
                break;
            case 'answer':
                gotAnswer(m.id, m.answer);
                break;
            case 'close':
                gotClose(m.id);
                break;
            case 'ice':
                gotICE(m.id, m.candidate);
                break;
            case 'maxbitrate':
                setMaxBitrate(m.id, m.audiorate, m.videorate);
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
            case 'error':
                displayError(m.message);
                break;
            default:
                console.warn('Unexpected server message', m.type);
                return;
            }
        };
    });
}

async function gotOffer(id, offer) {
    let c = down[id];
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
        }

        c.pc.ontrack = function(e) {
            c.stream = e.streams[0];
            setMedia(id);
        }
    }

    await c.pc.setRemoteDescription(offer);
    await addIceCandidates(c);
    let answer = await c.pc.createAnswer();
    if(!answer)
        throw new Error("Didn't create answer")
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
    await c.pc.setRemoteDescription(answer);
    await addIceCandidates(c);
}

function gotClose(id) {
    let c = down[id];
    if(!c)
        throw new Error('unknown down stream');
    delete(down[id]);
    c.close();
    delMedia(id);
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
        conn.iceCandidates.push(candidate)
}

let maxaudiorate, maxvideorate;

async function setMaxBitrate(id, audio, video) {
    let conn = up[id];
    if(!conn)
        throw new Error("Setting bitrate of unknown id");

    let senders = conn.pc.getSenders();
    for(let i = 0; i < senders.length; i++) {
        let s = senders[i];
        if(!s.track)
            return;
        let p = s.getParameters();
        let bitrate;
        if(s.track.kind == 'audio')
            bitrate = audio;
        else if(s.track.kind == 'video')
            bitrate = video;
        for(let j = 0; j < p.encodings.length; j++) {
            let e = p.encodings[j];
            if(bitrate)
                e.maxBitrate = bitrate;
            else
                delete(e.maxBitrate);
            await s.setParameters(p);
        }
    }

    if((audio && audio < 128000) || (video && video < 256000)) {
        let l = '';
        if(audio)
            l = `${Math.round(audio/1000)}kbps`
        if(video) {
            if(l)
                l = l + ' + ';
            l = l + `${Math.round(video/1000)}kbps`
        }
        setLabel(id, l)
    } else {
        setLabel(id);
    }
}

async function addIceCandidates(conn) {
    let promises = []
    conn.iceCandidates.forEach(c => {
        promises.push(conn.pc.addIceCandidate(c).catch(console.warn));
    });
    conn.iceCandidates = [];
    return await Promise.all(promises);
}

function send(m) {
    if(!m)
        throw(new Error('Sending null message'));
    return socket.send(JSON.stringify(m))
}

let users = {};

function addUser(id, name) {
    if(!name)
        name = null;
    if(id in users)
        throw new Error('Duplicate user id');
    users[id] = name;

    let div = document.getElementById('users');
    let anon = document.getElementById('anonymous-users');
    let user = document.createElement('div');
    user.id = 'user-' + id;
    user.textContent = name ? name : '(anon)';
    div.appendChild(user);
}

function delUser(id, name) {
    if(!name)
        name = null;
    if(!id in users)
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
    document.getElementById('presenterbox').disabled = !perm.present;
    document.getElementById('sharebox').disabled = !perm.present;
    displayUsername();
}

const urlRegexp = /https?:\/\/[-a-zA-Z0-9@:%/._\+~#=?]+[-a-zA-Z0-9@:%/_\+~#=]/g;

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

    document.getElementById('box').appendChild(container);

    if(box.scrollHeight > box.clientHeight) {
        box.scrollTop = box.scrollHeight - box.clientHeight;
    }

    return message;
}

function handleInput() {
    let username = getUsername();
    if(!username) {
        displayError("Sorry, you're anonymous, you cannot chat");
        return;
    }

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
            case '/op':
            case '/unop':
            case '/kick':
            case '/present':
            case '/unpresent':
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
            default:
                displayError('Uknown command ' + cmd);
                return;
            }
        }
    } else {
        message = data;
        me = false;
    }

    addToChatbox(myid, username, message, me);
    send({
        type: 'chat',
        username: username,
        value: message,
        me: me,
    });
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
}

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

async function newUpStream() {
    let id = randomid();
    if(up[id])
        throw new Error('Eek!');
    let pc = new RTCPeerConnection({
        iceServers: iceServers,
    });
    if(!pc)
        throw new Error("Couldn't create peer connection")
    up[id] = new Connection(id, pc);

    pc.onnegotiationneeded = e => negotiate(id);

    pc.onicecandidate = function(e) {
        if(!e.candidate)
            return;
        send({type: 'ice',
             id: id,
             candidate: e.candidate,
             });
    }

    pc.ontrack = console.error;

    return id;
}

async function negotiate(id) {
    let c = up[id];
    if(!c)
        throw new Error('unknown connection');
    let offer = await c.pc.createOffer({});
    if(!offer)
        throw(new Error("Didn't create offer"));
    await c.pc.setLocalDescription(offer);
    send({
        type: 'offer',
        id: id,
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

async function doConnect() {
    await serverConnect();
    await setLocalMedia();
    await setShareMedia();
}

document.getElementById('userform').onsubmit = async function(e) {
    e.preventDefault();
    let username = document.getElementById('username').value.trim();
    let password = document.getElementById('password').value;
    setUserPass(username, password);
    await doConnect();
}

document.getElementById('disconnectbutton').onclick = function(e) {
    socket.close();
}

function start() {
    group = decodeURIComponent(location.pathname.replace(/^\/[a-z]*\//, ''));
    let title = group.charAt(0).toUpperCase() + group.slice(1);
    if(group !== '') {
        document.title = title;
        document.getElementById('title').textContent = title
    }

    myid = randomid();

    getIceServers().catch(console.error).then(c => {
        document.getElementById('connectbutton').disabled = false;
    }).then(c => {
        let userpass = getUserPass();
        if(userpass)
            doConnect();
    });
}

start();
