// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

'use strict';

/** @type {string} */
let group;

/** @type {ServerConnection} */
let serverConnection;

/* Some browsers disable session storage when cookies are disabled,
   we fall back to a global variable. */
let fallbackUserPass = null;

function setUserPass(username, password) {
    let userpass = {username: username, password: password};
    try {
        window.sessionStorage.setItem('userpass', JSON.stringify(userpass));
        fallbackUserPass = null;
    } catch(e) {
        console.warn("Couldn't store password:", e);
        fallbackUserPass = {username: username, password: password};
    }
}

function getUserPass() {
    let userpass;
    try {
        let json = window.sessionStorage.getItem('userpass');
        userpass = JSON.parse(json);
    } catch(e) {
        console.warn("Couldn't retrieve password:", e);
        userpass = fallbackUserPass;
    }
    return userpass || null;
}

function getUsername() {
    let userpass = getUserPass();
    if(!userpass)
        return null;
    return userpass.username;
}

function showVideo() {
    let width = window.innerWidth;
    let video_container = document.getElementById('video-container');
    video_container.classList.remove('no-video');
    if (width <= 768)
        document.getElementById('collapse-video').style.display = "block";
}

function hideVideo(force) {
    let mediadiv = document.getElementById('peers');
    if (force === undefined) {
      force = false;
    }
    if (mediadiv.childElementCount > 0 && !force) {
      return;
    }
    let video_container = document.getElementById('video-container');
    video_container.classList.add('no-video');
}

function closeVideoControls() {
    // hide all video buttons used to switch video on mobile layout
    document.getElementById('switch-video').style.display = "";
    document.getElementById('collapse-video').style.display = "";
}

/**
  * @param{boolean} connected
  */
function setConnected(connected) {
    let statspan = document.getElementById('statspan');
    let userbox = document.getElementById('user');
    let connectionbox = document.getElementById('login-container');
    if(connected) {
        resetUsers();
        clearChat();
        statspan.textContent = 'Connected';
        statspan.classList.remove('disconnected');
        statspan.classList.add('connected');
        userbox.classList.remove('invisible');
        connectionbox.classList.add('invisible');
        displayUsername();
    } else {
        resetUsers();
        let userpass = getUserPass();
        document.getElementById('username').value =
            userpass ? userpass.username : '';
        document.getElementById('password').value =
            userpass ? userpass.password : '';
        statspan.textContent = 'Disconnected';
        statspan.classList.remove('connected');
        statspan.classList.add('disconnected');
        userbox.classList.add('invisible');
        connectionbox.classList.remove('invisible');
        clearUsername();
        displayError("Disconnected!", "error");
        hideVideo();
        closeVideoControls();
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
    delUpMediaKind(null);
    setConnected(false);
    if(code != 1000) {
        console.warn('Socket close', code, reason);
    }
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

// Store current browser viewport height in css variable
function setViewportHeight() {
    document.documentElement.style.setProperty('--vh', `${window.innerHeight/100}px`);
};
setViewportHeight();

// On resize and orientation change, we update viewport height
addEventListener('resize', setViewportHeight);
addEventListener('orientationchange', setViewportHeight);

document.getElementById('presentbutton').onclick = function(e) {
    e.preventDefault();
    addLocalMedia();
};

document.getElementById('unpresentbutton').onclick = function(e) {
    e.preventDefault();
    delUpMediaKind('local');
    resizePeers();
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

/** @type {boolean} */
let localMute = false;

function toggleLocalMute() {
    setLocalMute(!localMute);
}

function setLocalMute(mute) {
    localMute = mute;
    muteLocalTracks(localMute);
    let button = document.getElementById('mutebutton');
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

/** @returns {number} */
function getMaxVideoThroughput() {
    let v = document.getElementById('sendselect').value;
    switch(v) {
    case 'lowest':
        return 150000;
    case 'low':
        return 300000;
    case 'normal':
        return 700000;
    case 'unlimited':
        return null;
    default:
        console.error('Unknown video quality', v);
        return 700000;
    }
}

document.getElementById('sendselect').onchange = async function(e) {
    let t = getMaxVideoThroughput();
    for(let id in serverConnection.up) {
        let c = serverConnection.up[id];
        if(c.kind === 'local')
            await setMaxVideoThroughput(c, t);
    }
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
/** @type {boolean} */
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
 * @param {string} [id]
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
    c.onnegotiationcompleted = function() {
        setMaxVideoThroughput(c, getMaxVideoThroughput())
    }
    return c;
}

/**
 * @param {Stream} c
 * @param {number} [bps]
 */
async function setMaxVideoThroughput(c, bps) {
    let senders = c.pc.getSenders();
    for(let i = 0; i < senders.length; i++) {
        let s = senders[i];
        if(!s.track || s.track.kind !== 'video')
            continue;
        let p = s.getParameters();
        if(!p.encodings)
            p.encodings = [{}];
        p.encodings.forEach(e => {
            if(bps > 0)
                e.maxBitrate = bps;
            else
                delete e.maxBitrate;
        });
        try {
            await s.setParameters(p);
        } catch(e) {
            console.error(e);
        }
    }
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
        displayError(e);
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
    setButtonsVisibility();
}

async function addShareMedia(setup) {
    if(!getUserPass())
        return;

    let stream = null;
    try {
        stream = await navigator.mediaDevices.getDisplayMedia({video: true});
    } catch(e) {
        console.error(e);
        displayError(e);
        return;
    }

    let c = newUpStream();
    c.kind = 'screenshare';
    c.stream = stream;
    stream.getTracks().forEach(t => {
        let sender = c.pc.addTrack(t, stream);
        t.onended = e => {
            delUpMedia(c);
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
    try {
        delMedia(c.id);
    } catch(e) {
        console.warn(e);
    }
    c.close(true);
    delete(serverConnection.up[c.id]);
    setButtonsVisibility()
}

/**
 * delUpMediaKind reoves all up media of the given kind.  If kind is
 * falseish, it removes all up media.
 * @param {string} kind
*/
function delUpMediaKind(kind) {
    for(let id in serverConnection.up) {
        let c = serverConnection.up[id];
        if(kind && c.kind != kind)
            continue
        c.close(true);
        delMedia(id);
        delete(serverConnection.up[id]);
    }

    setButtonsVisibility();
    hideVideo();
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

    showVideo();
    resizePeers();
}

function delMedia(id) {
    let mediadiv = document.getElementById('peers');
    let peer = document.getElementById('peer-' + id);
    if(!peer)
        throw new Error('Removing unknown media');
    let media = document.getElementById('media-' + id);

    media.srcObject = null;
    mediadiv.removeChild(peer);

    resizePeers();
    hideVideo();
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
    let peers = document.getElementById('peers');
    let columns = Math.ceil(Math.sqrt(count));
    if (!count)
        // No video, nothing to resize.
        return;
    let size = 100 / columns;
    let container = document.getElementById("video-container")
    // Peers div has total padding of 30px, we remove 30 on offsetHeight
    let max_video_height = Math.trunc((peers.offsetHeight - 30) / columns);

    let media_list = document.getElementsByClassName("media");
    [].forEach.call(media_list, function (element) {
        element.style['max-height'] = max_video_height + "px";
    });

    if (count <= 2 && container.offsetHeight > container.offsetWidth) {
        peers.style['grid-template-columns'] = "repeat(1, 1fr)";
    } else {
        peers.style['grid-template-columns'] = `repeat(${columns}, 1fr)`;
    }
}

/** @type{Object.<string,string>} */
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
    if(serverConnection.permissions.present)
        displayMessage("Press Present to enable your camera or microphone",
                       "info");
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

/**
 * @typedef {Object} lastMessage
 * @property {string} [nick]
 * @property {string} [peerId]
 */

/** @type {lastMessage} */
let lastMessage = {};

function addToChatbox(peerId, nick, kind, message){
    let userpass = getUserPass();
    let row = document.createElement('div');
    row.classList.add('message-row');
    let container = document.createElement('div');
    container.classList.add('message');
    row.appendChild(container);
    if (userpass.username === nick) {
      container.classList.add('message-sender');
    }
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
    box.appendChild(row);
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
    let full_width = document.getElementById("mainrow").offsetWidth;
    let left = document.getElementById("left");
    let right = document.getElementById("right");

    let start_x = e.clientX;
    let start_width = parseFloat(left.offsetWidth);

    function start_drag(e) {
        let left_width = (start_width + e.clientX - start_x) * 100 / full_width;
        left.style.flex = left_width;
        right.style.flex = 100 - left_width;
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


function displayError(message, level, position, gravity) {
    var background = "linear-gradient(to right, #e20a0a, #df2d2d)";
    if (level === "info") {
      background = "linear-gradient(to right, #529518, #96c93d)";
    }
    if (level === "warning") {
      background = "linear-gradient(to right, #edd800, #c9c200)";
    }
    Toastify({
      text: message,
      duration: 4000,
      close: true,
      position: position ? position: 'center',
      gravity: gravity ? gravity : 'top',
      backgroundColor: background,
      className: level,
    }).showToast();
}

function displayWarning(message) {
    let level = "warning";
    return displayError(message, level);
}

function displayMessage(message) {
    return displayError(message, "info", "right", "bottom");
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
    let user_box = document.getElementById('userDropdown');
    if (user_box.classList.contains("show")) {
      user_box.classList.toggle("show");
    }
    
};

function openNav() {
    document.getElementById("sidebarnav").style.width = "250px";
}

function closeNav() {
    document.getElementById("sidebarnav").style.width = "0";
}

document.getElementById('sidebarCollapse').onclick = function(e) {
    document.getElementById("left-sidebar").classList.toggle("active");
    document.getElementById("mainrow").classList.toggle("full-width-active");
};

document.getElementById('openside').onclick = function(e) {
      e.preventDefault();
      let sidewidth = document.getElementById("sidebarnav").style.width;
      if (sidewidth !== "0px" && sidewidth !== "") {
          closeNav();
          return;
      } else {
          openNav();
      }
};

document.getElementById('user').onclick = function(e) {
    e.preventDefault();
    document.getElementById("userDropdown").classList.toggle("show");
};


document.getElementById('clodeside').onclick = function(e) {
    e.preventDefault();
    closeNav();
};

document.getElementById('collapse-video').onclick = function(e) {
    e.preventDefault();
    let width = window.innerWidth;
    if (width <= 768) {
      let user_box = document.getElementById('userDropdown');
      if (user_box.classList.contains("show")) {
        return;
      }
      // fixed div for small screen
      this.style.display = "";
      hideVideo(true);
      document.getElementById('switch-video').style.display = "block";
    }
};

document.getElementById('switch-video').onclick = function(e) {
    e.preventDefault();
    showVideo();
    this.style.display = "";
    document.getElementById('collapse-video').style.display = "block";
};

window.onclick = function(event) {
  let user_box = document.getElementById('userDropdown');
  if (user_box.classList.contains("show") && event.target.id != "user") {
      let parent = event.target;
      while (parent.id !== "main" && parent.id !== "userDropdown" &&
              parent.id !== "user" && parent.tagName !== "body") {
          parent = parent.parentNode;
      }
      if (parent.id !="userDropdown" && parent.id !== "user") {
          user_box.classList.toggle("show");
      }
  }
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


    let userpass = getUserPass();
    if(userpass)
        serverConnect();
    else {
      document.getElementById("user").classList.add('invisible');
      document.getElementById("login-container").classList.remove('invisible');
    }
}

start();
