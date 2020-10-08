// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

'use strict';

/** @type {string} */
let group;

/** @type {ServerConnection} */
let serverConnection;

/**
 * @typedef {Object} userpass
 * @property {string} username
 * @property {string} password
 */

/* Some browsers disable session storage when cookies are disabled,
   we fall back to a global variable. */
/**
 * @type {userpass}
 */
let fallbackUserPass = null;


/**
 * @param {string} username
 * @param {string} password
 */
function storeUserPass(username, password) {
    let userpass = {username: username, password: password};
    try {
        window.sessionStorage.setItem('userpass', JSON.stringify(userpass));
        fallbackUserPass = null;
    } catch(e) {
        console.warn("Couldn't store password:", e);
        fallbackUserPass = userpass;
    }
}

/**
 * Returns null if the user hasn't logged in yet.
 *
 * @returns {userpass}
 */
function getUserPass() {
    /** @type{userpass} */
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

/**
 * Return null if the user hasn't logged in yet.
 *
 * @returns {string}
 */
function getUsername() {
    let userpass = getUserPass();
    if(!userpass)
        return null;
    return userpass.username;
}

/**
 * @typedef {Object} settings
 * @property {boolean} [localMute]
 * @property {string} [video]
 * @property {string} [audio]
 * @property {string} [send]
 * @property {string} [request]
 * @property {boolean} [activityDetection]
 * @property {boolean} [blackboardMode]
 * @property {boolean} [studioMode]
 */

/** @type{settings} */
let fallbackSettings = null;

/**
 * @param {settings} settings
 */
function storeSettings(settings) {
    try {
        window.sessionStorage.setItem('settings', JSON.stringify(settings));
        fallbackSettings = null;
    } catch(e) {
        console.warn("Couldn't store password:", e);
        fallbackSettings = settings;
    }
}

/**
 * This always returns a dictionary.
 *
 * @returns {settings}
 */
function getSettings() {
    /** @type {settings} */
    let settings;
    try {
        let json = window.sessionStorage.getItem('settings');
        settings = JSON.parse(json);
    } catch(e) {
        console.warn("Couldn't retrieve password:", e);
        settings = fallbackSettings;
    }
    return settings || {};
}

/**
 * @param {settings} settings
 */
function updateSettings(settings) {
    let s = getSettings();
    for(let key in settings)
        s[key] = settings[key];
    storeSettings(s);
}

/**
 * @param {string} key
 * @param {any} value
 */
function updateSetting(key, value) {
    let s = {};
    s[key] = value;
    updateSettings(s);
}

/**
 * @param {string} key
 */
function delSetting(key) {
    let s = getSettings();
    if(!(key in s))
        return;
    delete(s[key]);
    storeSettings(s)
}

/**
 * @param {string} id
 */
function getSelectElement(id) {
    let elt = document.getElementById(id);
    if(!elt || !(elt instanceof HTMLSelectElement))
        throw new Error(`Couldn't find ${id}`);
    return elt;
}

/**
 * @param {string} id
 */
function getInputElement(id) {
    let elt = document.getElementById(id);
    if(!elt || !(elt instanceof HTMLInputElement))
        throw new Error(`Couldn't find ${id}`);
    return elt;
}

/**
 * @param {string} id
 */
function getButtonElement(id) {
    let elt = document.getElementById(id);
    if(!elt || !(elt instanceof HTMLButtonElement))
        throw new Error(`Couldn't find ${id}`);
    return elt;
}

function reflectSettings() {
    let settings = getSettings();
    let store = false;

    setLocalMute(settings.localMute);

    let videoselect = getSelectElement('videoselect');
    if(!settings.video || !selectOptionAvailable(videoselect, settings.video)) {
        settings.video = selectOptionDefault(videoselect);
        store = true;
    }
    videoselect.value = settings.video;

    let audioselect = getSelectElement('audioselect');
    if(!settings.audio || !selectOptionAvailable(audioselect, settings.audio)) {
        settings.audio = selectOptionDefault(audioselect);
        store = true;
    }
    audioselect.value = settings.audio;

    if(settings.request)
        getSelectElement('requestselect').value = settings.request;
    else {
        settings.request = getSelectElement('requestselect').value;
        store = true;
    }

    if(settings.send)
        getSelectElement('sendselect').value = settings.send;
    else {
        settings.send = getSelectElement('sendselect').value;
        store = true;
    }

    getInputElement('activitybox').checked = settings.activityDetection;

    getInputElement('blackboardbox').checked = settings.blackboardMode;

    getInputElement('studiobox').checked = settings.studioMode;

    if(store)
        storeSettings(settings);

}

function showVideo() {
    let width = window.innerWidth;
    let video_container = document.getElementById('video-container');
    video_container.classList.remove('no-video');
    if (width <= 768)
        document.getElementById('collapse-video').style.display = "block";
}

/**
 * @param {boolean} [force]
 */
function hideVideo(force) {
    let mediadiv = document.getElementById('peers');
    if(mediadiv.childElementCount > 0 && !force)
        return;
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
        getInputElement('username').value =
            userpass ? userpass.username : '';
        getInputElement('password').value =
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

/** @this {ServerConnection} */
function gotConnected() {
    setConnected(true);
    let up = getUserPass();
    this.login(up.username, up.password);
    this.join(group);
    this.request(getSettings().request);
}

/**
 * @this {ServerConnection}
 * @param {number} code
 * @param {string} reason
 */
function gotClose(code, reason) {
    delUpMediaKind(null);
    setConnected(false);
    if(code != 1000) {
        console.warn('Socket close', code, reason);
    }
}

/**
 * @this {ServerConnection}
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
    c.onstats = gotDownStats;
    if(getSettings().activityDetection)
        c.setStatsInterval(activityDetectionInterval);
}

// Store current browser viewport height in css variable
function setViewportHeight() {
    document.documentElement.style.setProperty(
        '--vh', `${window.innerHeight/100}px`,
    );
};
setViewportHeight();

// On resize and orientation change, we update viewport height
addEventListener('resize', setViewportHeight);
addEventListener('orientationchange', setViewportHeight);

getButtonElement('presentbutton').onclick = function(e) {
    e.preventDefault();
    addLocalMedia();
};

getButtonElement('unpresentbutton').onclick = function(e) {
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
    setVisibility('sharebutton', permissions.present &&
                  ('getDisplayMedia' in navigator.mediaDevices))
    setVisibility('unsharebutton', share);

    setVisibility('mediaoptions', permissions.present);
}

/**
 * @param {boolean} mute
 */
function setLocalMute(mute) {
    muteLocalTracks(mute);
    let button = document.getElementById('mutebutton');
    let icon = button.querySelector("span .fa");
    if(mute){
        icon.classList.add('fa-microphone-slash');
        icon.classList.remove('fa-microphone');
        button.classList.add('muted');
    } else {
        icon.classList.remove('fa-microphone-slash');
        icon.classList.add('fa-microphone');
        button.classList.remove('muted');
    }
}

getSelectElement('videoselect').onchange = function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLSelectElement))
        throw new Error('Unexpected type for this');
    updateSettings({video: this.value});
    changePresentation();
};

getSelectElement('audioselect').onchange = function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLSelectElement))
        throw new Error('Unexpected type for this');
    updateSettings({audio: this.value});
    changePresentation();
};

getInputElement('blackboardbox').onchange = function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLInputElement))
        throw new Error('Unexpected type for this');
    updateSettings({blackboardMode: this.checked});
    changePresentation();
}

getInputElement('studiobox').onchange = function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLInputElement))
        throw new Error('Unexpected type for this');
    updateSettings({studioMode: this.checked});
    changePresentation();
}

document.getElementById('mutebutton').onclick = function(e) {
    e.preventDefault();
    let localMute = getSettings().localMute;
    localMute = !localMute;
    updateSettings({localMute: localMute})
    setLocalMute(localMute);
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
    let v = getSettings().send;
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

getSelectElement('sendselect').onchange = async function(e) {
    if(!(this instanceof HTMLSelectElement))
        throw new Error('Unexpected type for this');
    updateSettings({send: this.value});
    let t = getMaxVideoThroughput();
    for(let id in serverConnection.up) {
        let c = serverConnection.up[id];
        if(c.kind === 'local')
            await setMaxVideoThroughput(c, t);
    }
}

getSelectElement('requestselect').onchange = function(e) {
    e.preventDefault();
    if(!(this instanceof HTMLSelectElement))
        throw new Error('Unexpected type for this');
    updateSettings({request: this.value});
    serverConnection.request(this.value);
};

const activityDetectionInterval = 200;
const activityDetectionPeriod = 700;
const activityDetectionThreshold = 0.2;

getInputElement('activitybox').onchange = function(e) {
    if(!(this instanceof HTMLInputElement))
        throw new Error('Unexpected type for this');
    updateSettings({activityDetection: this.checked});
    for(let id in serverConnection.down) {
        let c = serverConnection.down[id];
        if(this.checked)
            c.setStatsInterval(activityDetectionInterval);
        else {
            c.setStatsInterval(0);
            setActive(c, false);
        }
    }
}

/**
 * @this {Stream}
 * @param {Object<string,any>} stats
 */
function gotUpStats(stats) {
    let c = this;

    let text = '';

    c.pc.getSenders().forEach(s => {
        let tid = s.track && s.track.id;
        let stats = tid && c.stats[tid];
        let rate = stats && stats['outbound-rtp'] && stats['outbound-rtp'].rate;
        if(typeof rate === 'number') {
            if(text)
                text = text + ' + ';
            text = text + Math.round(rate / 1000) + 'kbps';
        }
    });

    setLabel(c, text);
}

/**
 * @param {Stream} c
 * @param {boolean} value
 */
function setActive(c, value) {
    let peer = document.getElementById('peer-' + c.id);
    if(value)
        peer.classList.add('peer-active');
    else
        peer.classList.remove('peer-active');
}

/**
 * @this {Stream}
 * @param {Object<string,any>} stats
 */
function gotDownStats(stats) {
    if(!getInputElement('activitybox').checked)
        return;

    let c = this;

    let maxEnergy = 0;

    c.pc.getReceivers().forEach(r => {
        let tid = r.track && r.track.id;
        let s = tid && stats[tid];
        let energy = s && s['track'] && s['track'].audioEnergy;
        if(typeof energy === 'number')
            maxEnergy = Math.max(maxEnergy, energy);
    });

    // totalAudioEnergy is defined as the integral of the square of the
    // volume, so square the threshold.
    if(maxEnergy > activityDetectionThreshold * activityDetectionThreshold) {
        c.userdata.lastVoiceActivity = Date.now();
        setActive(c, true);
    } else {
        let last = c.userdata.lastVoiceActivity;
        if(!last || Date.now() - last > activityDetectionPeriod)
            setActive(c, false);
    }
}

/**
 * @param {HTMLSelectElement} select
 * @param {string} label
 * @param {string} [value]
 */
function addSelectOption(select, label, value) {
    if(!value)
        value = label;
    for(let i = 0; i < select.children.length; i++) {
        let child = select.children[i];
        if(!(child instanceof HTMLOptionElement)) {
            console.warn('Unexpected select child');
            continue;
        }
        if(child.value === value) {
            if(child.label !== label) {
                child.label = label;
            }
            return;
        }
    }

    let option = document.createElement('option');
    option.value = value;
    option.textContent = label;
    select.appendChild(option);
}

/**
 * @param {HTMLSelectElement} select
 * @param {string} value
 */
function selectOptionAvailable(select, value) {
    let children = select.children;
    for(let i = 0; i < children.length; i++) {
        let child = select.children[i];
        if(!(child instanceof HTMLOptionElement)) {
            console.warn('Unexpected select child');
            continue;
        }
        if(child.value === value)
            return true;
    }
    return false;
}

/**
 * @param {HTMLSelectElement} select
 * @returns {string}
 */
function selectOptionDefault(select) {
    /* First non-empty option. */
    for(let i = 0; i < select.children.length; i++) {
        let child = select.children[i];
        if(!(child instanceof HTMLOptionElement)) {
            console.warn('Unexpected select child');
            continue;
        }
        if(child.value)
            return child.value;
    }
    /* The empty option is always available. */
    return '';
}

/* media names might not be available before we call getDisplayMedia.  So
   we call this twice, the second time to update the menu with user-readable
   labels. */
/** @type {boolean} */
let mediaChoicesDone = false;

/**
 * @param{boolean} done
 */
async function setMediaChoices(done) {
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
            addSelectOption(getSelectElement('videoselect'),
                            label, d.deviceId);
            cn++;
        } else if(d.kind === 'audioinput') {
            if(!label)
                label = `Microphone ${mn}`;
            addSelectOption(getSelectElement('audioselect'),
                            label, d.deviceId);
            mn++;
        }
    });

    mediaChoicesDone = done;
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

    let settings = getSettings();

    let audio = settings.audio ? {deviceId: settings.audio} : false;
    let video = settings.video ? {deviceId: settings.video} : false;

    if(audio) {
        if(settings.studioMode) {
            audio.echoCancellation = false;
            audio.noiseSuppression = false;
            audio.channelCount = 2;
            audio.latency = 0.01;
            audio.autoGainControl = false;
        }
    }

    if(video) {
        if(settings.blackboardMode) {
            video.width = { min: 640, ideal: 1920 };
            video.height = { min: 400, ideal: 1080 };
        }
    }

    let old = id && serverConnection.up[id];

    if(!audio && !video) {
        if(old)
            delUpMedia(old);
        return;
    }

    if(old)
        stopUpMedia(old);

    let constraints = {audio: audio, video: video};
    /** @type {MediaStream} */
    let stream = null;
    try {
        stream = await navigator.mediaDevices.getUserMedia(constraints);
    } catch(e) {
        displayError(e);
        if(old)
            delUpMedia(old);
        return;
    }

    setMediaChoices(true);

    let c = newUpStream(id);

    c.kind = 'local';
    c.stream = stream;
    let mute = getSettings().localMute;
    stream.getTracks().forEach(t => {
        c.labels[t.id] = t.kind
        if(t.kind == 'audio') {
            if(mute)
                t.enabled = false;
        } else if(t.kind == 'video') {
            if(settings.blackboardMode) {
                /** @ts-ignore */
                t.contentHint = 'detail';
            }
        }
        c.pc.addTrack(t, stream);
    });

    c.onstats = gotUpStats;
    c.setStatsInterval(2000);
    await setMedia(c, true);
    setButtonsVisibility();
}

async function addShareMedia() {
    if(!getUserPass())
        return;

    /** @type {MediaStream} */
    let stream = null;
    try {
        if(!('getDisplayMedia' in navigator.mediaDevices))
            throw new Error('Your browser does not support screen sharing');
        /** @ts-ignore */
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
        c.pc.addTrack(t, stream);
        t.onended = e => {
            delUpMedia(c);
        };
        c.labels[t.id] = 'screenshare';
    });
    c.onstats = gotUpStats;
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

/**
 * @param {string} kind
 */
function findUpMedia(kind) {
    for(let id in serverConnection.up) {
        if(serverConnection.up[id].kind === kind)
            return id;
    }
    return null;
}

/**
 * @param {boolean} mute
 */
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

    let media = /** @type {HTMLVideoElement} */
        (document.getElementById('media-' + c.id));
    if(!media) {
        media = document.createElement('video');
        media.id = 'media-' + c.id;
        media.classList.add('media');
        media.autoplay = true;
        /** @ts-ignore */
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

/**
 * @param {string} id
 */
function delMedia(id) {
    let mediadiv = document.getElementById('peers');
    let peer = document.getElementById('peer-' + id);
    if(!peer)
        throw new Error('Removing unknown media');

    let media = /** @type{HTMLVideoElement} */
        (document.getElementById('media-' + id));

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
    let container = document.getElementById("video-container")
    // Peers div has total padding of 30px, we remove 30 on offsetHeight
    let max_video_height = Math.trunc((peers.offsetHeight - 30) / columns);

    let media_list = document.getElementsByClassName("media");
    for(let i = 0; i < media_list.length; i++) {
        let media = media_list[i];
        if(!(media instanceof HTMLMediaElement)) {
            console.warn('Unexpected media');
            continue;
        }
        media.style['max_height'] = max_video_height + "px";
    }

    if (count <= 2 && container.offsetHeight > container.offsetWidth) {
        peers.style['grid-template-columns'] = "repeat(1, 1fr)";
    } else {
        peers.style['grid-template-columns'] = `repeat(${columns}, 1fr)`;
    }
}

/** @type{Object<string,string>} */
let users = {};

/**
 * Lexicographic order, with case differences secondary.
 * @param{string} a
 * @param{string} b
 */
function stringCompare(a, b) {
    let la = a.toLowerCase()
    let lb = b.toLowerCase()
    if(la < lb)
        return -1;
    else if(la > lb)
        return +1;
    else if(a < b)
        return -1;
    else if(a > b)
        return +1;
    return 0
}

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
    user.classList.add("user-p");
    user.textContent = name ? name : '(anon)';

    if(name) {
        let us = div.children;
        for(let i = 0; i < us.length; i++) {
            let child = us[i];
            let childname = users[child.id.slice('user-'.length)] || null;
            if(!childname || stringCompare(childname, name) > 0) {
                div.insertBefore(user, child);
                return;
            }
        }
    }
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
 * @param {Object<string,boolean>} perms
 */
function gotPermissions(perms) {
    displayUsername();
    setButtonsVisibility();
    if(serverConnection.permissions.present)
        displayMessage("Press Present to enable your camera or microphone");
}

const urlRegexp = /https?:\/\/[-a-zA-Z0-9@:%/._\\+~#=?]+[-a-zA-Z0-9@:%/_\\+~#=]/g;

/**
 * @param {string} line
 * @returns {(Text|HTMLElement)[]}
 */
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

/**
 * @param {string[]} lines
 * @returns {HTMLElement}
 */
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
 * @param {number} time
 * @returns {string}
 */
function formatTime(time) {
    let delta = Date.now() - time;
    let date = new Date(time);
    if(delta >= 0)
        return date.toTimeString().slice(null, 8);
    return date.toLocaleString();
}

/**
 * @typedef {Object} lastMessage
 * @property {string} [nick]
 * @property {string} [peerId]
 * @property {string} [dest]
 */

/** @type {lastMessage} */
let lastMessage = {};

/**
 * @param {string} peerId
 * @param {string} nick
 * @param {number} time
 * @param {string} kind
 * @param {string} message
 */
function addToChatbox(peerId, dest, nick, time, kind, message) {
    let userpass = getUserPass();
    let row = document.createElement('div');
    row.classList.add('message-row');
    let container = document.createElement('div');
    container.classList.add('message');
    row.appendChild(container);
    if(!peerId)
        container.classList.add('message-system');
    if(userpass.username === nick)
        container.classList.add('message-sender');
    if(dest)
        container.classList.add('message-private');

    if(kind !== 'me') {
        let p = formatLines(message.split('\n'));
        if(lastMessage.nick !== (nick || null) ||
           lastMessage.peerId !== peerId ||
           lastMessage.dest !== (dest || null)) {
            let header = document.createElement('p');
            let user = document.createElement('span');
            user.textContent = dest ?
                `${nick||'(anon)'} \u2192 ${users[dest]||'(anon)'}` :
                (nick || '(anon)');
            user.classList.add('message-user');
            header.appendChild(user);
            if(time) {
                let tm = document.createElement('span');
                tm.textContent = formatTime(time);
                tm.classList.add('message-time');
                header.appendChild(tm);
            }
            header.classList.add('message-header');
            container.appendChild(header);
        }
        p.classList.add('message-content');
        container.appendChild(p);
        lastMessage.nick = (nick || null);
        lastMessage.peerId = peerId;
        lastMessage.dest = (dest || null);
    } else {
        let asterisk = document.createElement('span');
        asterisk.textContent = '*';
        asterisk.classList.add('message-me-asterisk');
        let user = document.createElement('span');
        user.textContent = nick || '(anon)';
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
        lastMessage = {};
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

/**
 * parseCommand splits a string into two space-separated parts.  The first
 * part may be quoted and may include backslash escapes.
 *
 * @param {string} line
 * @returns {string[]}
 */
function parseCommand(line) {
    let i = 0;
    while(i < line.length && line[i] === ' ')
        i++;
    let start = ' ';
    if(i < line.length && line[i] === '"' || line[i] === "'") {
        start = line[i];
        i++;
    }
    let first = "";
    while(i < line.length) {
        if(line[i] === start) {
            if(start !== ' ')
                i++;
            break;
        }
        if(line[i] === '\\' && i < line.length - 1)
            i++;
        first = first + line[i];
        i++;
    }

    while(i < line.length && line[i] === ' ')
        i++;
    return [first, line.slice(i)];
}

function handleInput() {
    let input = /** @type {HTMLTextAreaElement} */
        (document.getElementById('input'));
    let data = input.value;
    input.value = '';

    let message, me;

    if(data === '')
        return;

    if(data[0] === '/') {
        if(data.length > 1 && data[1] === '/') {
            message = data.substring(1);
            me = false;
        } else {
            let cmd, rest;
            let space = data.indexOf(' ');
            if(space < 0) {
                cmd = data;
                rest = '';
            } else {
                cmd = data.slice(0, space);
                rest = data.slice(space + 1);
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
            case '/set':
                if(!rest) {
                    let settings = getSettings();
                    let s = "";
                    for(let key in settings)
                        s = s + `${key}: ${JSON.stringify(settings[key])}\n`
                    addToChatbox(null, null, null, Date.now(), null, s);
                    return;
                }
                let parsed = parseCommand(rest);
                let value;
                if(parsed[1]) {
                    try {
                        value = JSON.parse(parsed[1])
                    } catch(e) {
                        displayError(e);
                        return;
                    }
                } else {
                    value = true;
                }
                updateSetting(parsed[0], value);
                reflectSettings();
                return;
            case '/unset':
                delSetting(rest.trim());
                return;
            case '/lock':
            case '/unlock':
                if(!serverConnection.permissions.op) {
                    displayError("You're not an operator");
                    return;
                }
                serverConnection.groupAction(cmd.slice(1), rest);
                return;
            case '/record':
            case '/unrecord':
                if(!serverConnection.permissions.record) {
                    displayError("You're not allowed to record");
                    return;
                }
                serverConnection.groupAction(cmd.slice(1));
                return;
            case '/msg':
            case '/op':
            case '/unop':
            case '/kick':
            case '/present':
            case '/unpresent': {
                let parsed = parseCommand(rest);
                let id;
                if(parsed[0] in users) {
                    id = parsed[0];
                } else {
                    for(let i in users) {
                        if(users[i] === parsed[0]) {
                            id = i;
                            break;
                        }
                    }
                }
                if(!id) {
                    displayError('Unknown user ' + parsed[0]);
                    return;
                }
                if(cmd === '/msg') {
                    let username = getUsername();
                    serverConnection.chat(username, '', id, parsed[1]);
                    addToChatbox(serverConnection.id,
                                 id, username, Date.now(), '', parsed[1]);
                } else {
                    serverConnection.userAction(cmd.slice(1), id, parsed[1]);
                }
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

    if(!serverConnection || !serverConnection.socket) {
        displayError("Not connected.");
        return;
    }

    let username = getUsername();
    try {
        serverConnection.chat(username, me ? 'me' : '', '', message);
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
    let start_width = left.offsetWidth;

    function start_drag(e) {
        let left_width = (start_width + e.clientX - start_x) * 100 / full_width;
        left.style.flex = left_width.toString();
        right.style.flex = (100 - left_width).toString();
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

/**
 * @param {unknown} message
 * @param {string} [level]
 */
function displayError(message, level) {
    if(!level)
        level = "error";

    var background = 'linear-gradient(to right, #e20a0a, #df2d2d)';
    var position = 'center';
    var gravity = 'top';

    switch(level) {
    case "info":
        background = 'linear-gradient(to right, #529518, #96c93d)';
        position = 'right';
        gravity = 'bottom';
        break;
    case "warning":
        background = "linear-gradient(to right, #edd800, #c9c200)";
        break;
    }

    /** @ts-ignore */
    Toastify({
        text: message,
        duration: 4000,
        close: true,
        position: position,
        gravity: gravity,
        backgroundColor: background,
        className: level,
    }).showToast();
}

/**
 * @param {unknown} message
 */
function displayWarning(message) {
    return displayError(message, "warning");
}

/**
 * @param {unknown} message
 */
function displayMessage(message) {
    return displayError(message, "info");
}

document.getElementById('userform').onsubmit = function(e) {
    e.preventDefault();
    let username = getInputElement('username').value.trim();
    let password = getInputElement('password').value;
    storeUserPass(username, password);
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
    if(!(this instanceof HTMLElement))
        throw new Error('Unexpected type for this');
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
    if(!(this instanceof HTMLElement))
        throw new Error('Unexpected type for this');
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

async function serverConnect() {
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
    let url = `ws${location.protocol === 'https:' ? 's' : ''}://${location.host}/ws`;
    try {
        await serverConnection.connect(url);
    } catch(e) {
        console.error(e);
        displayError(e.message ? e.message : "Couldn't connect to " + url);
    }
}

function start() {
    group = decodeURIComponent(location.pathname.replace(/^\/[a-z]*\//, ''));
    let title = group.charAt(0).toUpperCase() + group.slice(1);
    if(group !== '') {
        document.title = title;
        document.getElementById('title').textContent = title;
    }

    setMediaChoices(false).then(e => reflectSettings());

    document.getElementById("user").classList.add('invisible');
    document.getElementById("login-container").classList.remove('invisible');
}

start();
