/* Galene client example. */

/**
 * The main function.
 *
 * @param {string} url
 */
async function start(url) {
    // fetch the group information
    let r = await fetch(url + ".status");
    if(!r.ok) {
        throw new Error(`${r.status} ${r.statusText}`);
    }
    let status = await r.json();

    // parse a token in the URL.
    let token = null;
    let parms = new URLSearchParams(window.location.search);
    if(parms.has('token'))
        token = parms.get('token');

    // connect to the server
    if(token) {
        serverConnect(status, token);
    } else if(status.authPortal) {
        window.location.href = groupStatus.authPortal
        return;
    } else {
        serverConnect(status, null);
    }
}

/**
 * Display the connection status.
 *
 * @parm {string} status
 */
function displayStatus(status) {
    let c = document.getElementById('status');
    c.textContent = status;
}

/**
 * Connect to the server.
 *
 * @parm {Object} status
 * @parm {string} token
 */
function serverConnect(status, token) {
    // create the connection to the server
    let conn = new ServerConnection();
    conn.onconnected = async function() {
        displayStatus('Connected');
        let creds = token ?
            {type: 'token', token: token} :
            {type: 'password', password: ''};
        // join the group and wait for the onjoined callback
        await this.join("public", "example-user", creds);
    };
    conn.onchat = onChat;
    conn.onusermessage = onUserMessage;
    conn.ondownstream = onDownStream;
    conn.onclose = function() {
        displayStatus('Disconnected');
    }
    conn.onjoined = onJoined;

    // connect and wait for the onconnected callback
    conn.connect(status.endpoint);
}

/**
 * Called whenever we receive a chat message.
 *
 * @this {ServerConnection}
 * @parm {string} username
 * @parm {string} message
 */
function onChat(id, dest, username, time, privileged, history, kind, message) {
    let p = document.createElement('p');
    p.textContent = `${username}${dest ? ' → ' + dest : ''}: ${message}`;
    let container = document.getElementById('chat');
    container.appendChild(p);
}

/**
 * Called whenever we receive a user message.
 *
 * @this {ServerConnection}
 * @parm {string} username
 * @parm {string} message
 * @parm {string} kind
 */
function onUserMessage(id, dest, username, time, privileged, kind, error, message) {
    switch(kind) {
    case 'kicked':
    case 'error':
    case 'warning':
    case 'info':
        if(!privileged) {
            console.error(`Got unprivileged message of kind ${kind}`);
            return;
        }
        displayError(message);
        break;
    case 'clearchat':
        if(!privileged) {
            console.error(`Got unprivileged message of kind ${kind}`);
            return;
        }
        document.getElementById('chat').textContent = '';
        break;
    }
}


/**
 * Find the camera stream, if any.
 *
 * @parm {string} conn
 * @returns {Stream}
 */
function cameraStream(conn) {
    for(let id in conn.up) {
        let s = conn.up[id];
        if(s.label === 'camera')
            return s;
    }
    return null;
}

/**
 * Enable or disable the show/hide button.
 *
 * @parm{ServerConnection} conn
 * @parm{boolean} enable
 */
function enableShow(conn, enable) {
    let b = /** @type{HTMLButtonElement} */(document.getElementById('show'));
    if(enable) {
        b.onclick = function() {
            let s = cameraStream(conn);
            if(!s)
                showCamera(conn);
            else
                hide(conn, s);
        }
        b.disabled = false;
    } else {
        b.disabled = true;
        b.onclick = null;
    }
}

/**
 * Called when we join or leave a group.
 *
 * @this {ServerConnection}
 * @parm {string} kind
 * @parm {string} message}
 */
async function onJoined(kind, group, perms, status, data, error, message) {
    switch(kind) {
    case 'fail':
        displayError(message);
        enableShow(this, false);
        this.close();
        break;
    case 'redirect':
        this.close();
        document.location.href = message;
        return;
    case 'leave':
        displayStatus('Connected');
        enableShow(this, false);
        this.close();
        break;
    case 'join':
    case 'change':
        displayStatus(`Connected as ${this.username} in group ${this.group}.`);
        enableShow(this, true);
        // request videos from the server
        this.request({'': ['audio', 'video']});
        break;
    default:
        displayError(`Unexpected state ${kind}.`);
        this.close();
        break;
    }
}

/**
 * Create a video element.  We encode the stream's id in the element's id
 * in order to avoid having a global hash table that maps ids to video
 * elements.
 *
 * @parm {string} id
 * @returns {HTMLVideoElement}
 */
function makeVideoElement(id) {
    let v = document.createElement('video');
    v.id = 'video-' + id;
    let container = document.getElementById('videos');
    container.appendChild(v);
    return v;
}

/**
 * Find the video element that shows a given id.
 *
 * @parm {string} id
 * @returns {HTMLVideoElement}
 */
function getVideoElement(id) {
    let v = document.getElementById('video-' + id);
    return /** @type{HTMLVideoElement} */(v);
}

/**
 * Enable the camera and broadcast yourself to the group.
 *
 * @parm {ServerConnection} conn
 */
async function showCamera(conn) {
    let ms = await navigator.mediaDevices.getUserMedia({audio: true, video: true});

    /* Send the new stream to the server */
    let s = conn.newUpStream();
    s.label = 'camera';
    s.setStream(ms);
    let v = makeVideoElement(s.localId);
    s.onclose = function(replace) {
        s.stream.getTracks().forEach(t => t.stop());
        v.srcObject = null;
        v.parentNode.removeChild(v);
    }

    function addTrack(t) {
        t.oneneded = function(e) {
            ms.onaddtrack = null;
            s.onremovetrack = null;
            s.close();
        }
        s.pc.addTransceiver(t, {
            direction: 'sendonly',
            streams: [ms],
        });
    }

    // Make sure all future tracks are added.
    s.onaddtrack = function(e) {
        addTrack(e.track);
    }
    // Add any existing tracks.
    ms.getTracks().forEach(addTrack);

    // Connect the MediaStream to the video element and start playing.
    v.srcObject = ms;
    v.muted = true;
    v.play();
}

/**
 * Stop broadcasting.
 *
 * @parm {ServerConnection} conn
 * @parm {Stream} s
 */
async function hide(conn, s) {
    s.stream.getTracks().forEach(t => t.stop());
    s.close();
}

/**
 * Called when the server pushes a stream.
 *
 * @this {ServerConnection}
 * @parm {Stream} c
 */
function onDownStream(s) {
    s.onclose = function(replace) {
        let v = getVideoElement(s.localId);
        v.srcObject = null;
        v.parentNode.removeChild(v);
    }
    s.ondowntrack = function(track, transceiver, stream) {
        let v = getVideoElement(s.localId);
        if(v.srcObject !== stream)
            v.srcObject = stream;
    }

    let v = makeVideoElement(s.localId);
    v.srcObject = s.stream;
    v.play();
}

/**
 * Display an error message.
 *
 * @parm {string} message
 */
function displayError(message) {
    document.getElementById('error').textContent = message;
}

document.getElementById('start').onclick = async function(e) {
    let button = /** @type{HTMLButtonElement} */(this);
    button.hidden = true;
    try {
        await start("/group/public/");
    } catch(e) {
        displayError(e);
    };
}
