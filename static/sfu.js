// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

'use strict';

let group;
let serverConnection;

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
        resetUsers();
        clearChat();
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
        clearUsername();
    }
}

function gotConnected() {
    setConnected(true);
    let up = getUserPass();
    this.login(up.username, up.password);
    this.join(group);
    this.request(document.getElementById('requestselect').value);
}

function gotClose(code, reason) {
    setConnected(false);
    if(code != 1000)
        console.warn('Socket close', code, reason);
}

/**
 * @param {Stream} c
 */
function gotDownStream(c) {
    c.onclose = function() {
        delMedia(c.id);
    };
    c.onerror = function(e) {
        console.error(e);
        displayError(e);
    }
    c.ondowntrack = function(track, transceiver, label, stream) {
        setMedia(c, false);
    }
    c.onlabel = function(label) {
        setLabel(c);
    }
    c.onstatus = function(status) {
        setMediaStatus(c);
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

/**
 * @param {string} id
 * @param {boolean} visible
 */
function setVisibility(id, visible) {
    let elt = document.getElementById(id);
    if(visible)
        elt.classList.remove('invisible');
    else
        elt.classList.add('invisible');
}

function setButtonsVisibility() {
    let permissions = serverConnection.permissions;
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
    serverConnection.request(this.value);
};

function displayStats(stats) {
    let c = this;

    let text = '';

    c.pc.getSenders().forEach(s => {
        let tid = s.track && s.track.id;
        let stats = tid && c.stats[tid];
        if(stats && stats.rate > 0) {
            if(text)
                text = text + ' + ';
            text = text + Math.round(stats.rate / 1000) + 'kbps';
        }
    });

    setLabel(c, text);
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

/**
 * @param {string} id
 */
function newUpStream(id) {
    let c = serverConnection.newUpStream(id);
    c.onstatus = function(status) {
        setMediaStatus(c);
    }
    c.onerror = function(e) {
        console.error(e);
        displayError(e);
        delUpMedia(c);
    }
    c.onabort = function() {
        delUpMedia(c);
    }
    return c;
}

/**
 * @param {string} [id]
 */
async function addLocalMedia(id) {
    if(!getUserPass())
        return;

    let audio = mapMediaOption(document.getElementById('audioselect').value);
    let video = mapMediaOption(document.getElementById('videoselect').value);

    let old = id && serverConnection.up[id];

    if(!audio && !video) {
        if(old)
            delUpMedia(old);
        return;
    }

    if(old)
        stopUpMedia(old);

    let constraints = {audio: audio, video: video};
    let stream = null;
    try {
        stream = await navigator.mediaDevices.getUserMedia(constraints);
    } catch(e) {
        console.error(e);
        if(old)
            delUpMedia(old);
        return;
    }

    setMediaChoices();

    let c = newUpStream(id);

    c.kind = 'local';
    c.stream = stream;
    stream.getTracks().forEach(t => {
        c.labels[t.id] = t.kind
        if(t.kind == 'audio' && localMute)
            t.enabled = false;
        let sender = c.pc.addTrack(t, stream);
    });
    c.onstats = displayStats;
    c.setStatsInterval(2000);
    await setMedia(c, true);
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

    let c = await serverConnection.newUpStream();
    c.kind = 'screenshare';
    c.stream = stream;
    stream.getTracks().forEach(t => {
        let sender = c.pc.addTrack(t, stream);
        t.onended = e => {
            delUpMedia(c.id);
        };
        c.labels[t.id] = 'screenshare';
    });
    c.onstats = displayStats;
    c.setStatsInterval(2000);
    await setMedia(c, true);
    setButtonsVisibility()
}

/**
 * @param {Stream} c
 */
function stopUpMedia(c) {
    if(!c.stream)
        return;
    c.stream.getTracks().forEach(t => {
        try {
            t.stop();
        } catch(e) {
        }
    });
}

/**
 * @param {Stream} c
 */
function delUpMedia(c) {
    stopUpMedia(c);
    delMedia(c.id);
    c.close(true);
    delete(serverConnection.up[c.id]);
    setButtonsVisibility()
}

function delUpMediaKind(kind) {
    for(let id in serverConnection.up) {
        let c = serverConnection.up[id];
        if(c.kind != kind)
            continue
        c.close(true);
        delMedia(id);
        delete(serverConnection.up[id]);
    }

    setButtonsVisibility()
}

function findUpMedia(kind) {
    for(let id in serverConnection.up) {
        if(serverConnection.up[id].kind === kind)
            return id;
    }
    return null;
}

function muteLocalTracks(mute) {
    if(!serverConnection)
        return;
    for(let id in serverConnection.up) {
        let c = serverConnection.up[id];
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

/**
 * @param {Stream} c
 * @param {boolean} isUp
 */
function setMedia(c, isUp) {
    let peersdiv = document.getElementById('peers');

    let div = document.getElementById('peer-' + c.id);
    if(!div) {
        div = document.createElement('div');
        div.id = 'peer-' + c.id;
        div.classList.add('peer');
        peersdiv.appendChild(div);
    }

    let media = document.getElementById('media-' + c.id);
    if(!media) {
        media = document.createElement('video');
        media.id = 'media-' + c.id;
        media.classList.add('media');
        media.autoplay = true;
        media.playsinline = true;
        media.controls = true;
        if(isUp)
            media.muted = true;
        div.appendChild(media);
    }

    let label = document.getElementById('label-' + c.id);
    if(!label) {
        label = document.createElement('div');
        label.id = 'label-' + c.id;
        label.classList.add('label');
        div.appendChild(label);
    }

    media.srcObject = c.stream;
    setLabel(c);
    setMediaStatus(c);

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

/**
 * @param {Stream} c
 */
function setMediaStatus(c) {
    let state = c && c.pc && c.pc.iceConnectionState;
    let good = state === 'connected' || state === 'completed';

    let media = document.getElementById('media-' + c.id);
    if(!media) {
        console.warn('Setting status of unknown media.');
        return;
    }
    if(good)
        media.classList.remove('media-failed');
    else
        media.classList.add('media-failed');
}


/**
 * @param {Stream} c
 * @param {string} [fallback]
 */
function setLabel(c, fallback) {
    let label = document.getElementById('label-' + c.id);
    if(!label)
        return;
    let l = c.label;
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
    let count =
        Object.keys(serverConnection.up).length +
        Object.keys(serverConnection.down).length;
    let columns = Math.ceil(Math.sqrt(count));
    document.getElementById('peers').style['grid-template-columns'] =
        `repeat(${columns}, 1fr)`;
}

let users = {};

/**
 * @param {string} id
 * @param {string} name
 */
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

/**
 * @param {string} id
 * @param {string} name
 */
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

/**
 * @param {string} id
 * @param {string} kind
 * @param {string} name
 */
function gotUser(id, kind, name) {
    switch(kind) {
    case 'add':
        addUser(id, name);
        break;
    case 'delete':
        delUser(id, name);
        break;
    default:
        console.warn('Unknown user kind', kind);
        break;
    }
}

function displayUsername() {
    let userpass = getUserPass();
    let text = '';
    if(userpass && userpass.username)
        text = 'as ' + userpass.username;
    if(serverConnection.permissions.op && serverConnection.permissions.present)
        text = text + ' (op, presenter)';
    else if(serverConnection.permissions.op)
        text = text + ' (op)';
    else if(serverConnection.permissions.present)
        text = text + ' (presenter)';
    document.getElementById('userspan').textContent = text;
}

function clearUsername() {
    document.getElementById('userspan').textContent = '';
}

/**
 * @param {Object.<string,boolean>} perms
 */
function gotPermissions(perms) {
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

function addToChatbox(peerId, nick, kind, message){
    let container = document.createElement('div');
    container.classList.add('message');
    if(kind !== 'me') {
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

function clearChat() {
    lastMessage = {};
    document.getElementById('box').textContent = '';
}

function handleInput() {
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
                serverConnection.close();
                return;
            case '/clear':
                if(!serverConnection.permissions.op) {
                    displayError("You're not an operator");
                    return;
                }
                serverConnection.groupAction('clearchat');
                return;
            case '/lock':
            case '/unlock':
                if(!serverConnection.permissions.op) {
                    displayError("You're not an operator");
                    return;
                }
                serverConnection.groupAction(cmd.slice(1));
                return;
            case '/record':
            case '/unrecord':
                if(!serverConnection.permissions.record) {
                    displayError("You're not allowed to record");
                    return;
                }
                serverConnection.groupAction(cmd.slice(1));
                return;
            case '/op':
            case '/unop':
            case '/kick':
            case '/present':
            case '/unpresent': {
                if(!serverConnection.permissions.op) {
                    displayError("You're not an operator");
                    return;
                }
                let id;
                if(rest in users) {
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
                serverConnection.userAction(cmd.slice(1), id)
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

    let username = getUsername();
    if(!username) {
        displayError("Sorry, you're anonymous, you cannot chat");
        return;
    }

    try {
        serverConnection.chat(username, me ? 'me' : '', message);
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

document.getElementById('userform').onsubmit = function(e) {
    e.preventDefault();
    let username = document.getElementById('username').value.trim();
    let password = document.getElementById('password').value;
    setUserPass(username, password);
    serverConnect();
};

document.getElementById('disconnectbutton').onclick = function(e) {
    serverConnection.close();
};

function serverConnect() {
    serverConnection = new ServerConnection();
    serverConnection.onconnected = gotConnected;
    serverConnection.onclose = gotClose;
    serverConnection.ondownstream = gotDownStream;
    serverConnection.onuser = gotUser;
    serverConnection.onpermissions = gotPermissions;
    serverConnection.onchat = addToChatbox;
    serverConnection.onclearchat = clearChat;
    serverConnection.onusermessage = function(kind, message) {
        if(kind === 'error')
            displayError(`The server said: ${message}`);
        else
            displayWarning(`The server said: ${message}`);
    }
    return serverConnection.connect(`ws${location.protocol === 'https:' ? 's' : ''}://${location.host}/ws`);
}

function start() {
    group = decodeURIComponent(location.pathname.replace(/^\/[a-z]*\//, ''));
    let title = group.charAt(0).toUpperCase() + group.slice(1);
    if(group !== '') {
        document.title = title;
        document.getElementById('title').textContent = title;
    }

    setLocalMute(localMute);

    document.getElementById('connectbutton').disabled = false;

    let userpass = getUserPass();
    if(userpass)
        serverConnect();
}

start();
