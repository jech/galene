// Copyright (c) 2020 by Juliusz Chroboczek.

// This is not open source software.  Copy it, and I'll break into your
// house and tell your three year-old that Santa doesn't exist.

'use strict';

/**
 * toHex formats an array as a hexadecimal string.
 * @returns {string}
 */
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

/** randomid returns a random string of 32 hex digits (16 bytes).
 * @returns {string}
 */
function randomid() {
    let a = new Uint8Array(16);
    crypto.getRandomValues(a);
    return toHex(a);
}

/**
 * ServerConnection encapsulates a websocket connection to the server and
 * all the associated streams.
 * @constructor
 */
function ServerConnection() {
    /**
     * The id of this connection.
     * @type {string}
     */
    this.id = randomid();
    /**
     * The group that we have joined, or nil if we haven't joined yet.
     * @type {string}
     */
    this.group = null;
    /**
     * The underlying websocket.
     * @type {WebSocket}
     */
    this.socket = null;
    /**
     * The set of all up streams, indexed by their id.
     * @type {Object.<string,Stream>}
     */
    this.up = {};
    /**
     * The set of all down streams, indexed by their id.
     * @type {Object.<string,Stream>}
     */
    this.down = {};
    /**
     * The ICE configuration used by all associated streams.
     * @type {Array.<Object>}
     */
    this.iceServers = [];
    /**
     * The permissions granted to this connection.
     * @type {Object.<string,boolean>}
     */
    this.permissions = {};

    /* Callbacks */

    /**
     * onconnected is called when the connection has been established
     * @type{function(): any}
     */
    this.onconnected = null;
    /**
     * onclose is called when the connection is closed
     * @type{function(number, string): any}
     */
    this.onclose = null;
    /**
     * onuser is called whenever a user is added or removed from the group
     * @type{function(string, string, string): any}
     */
    this.onuser = null;
    /**
     * onpermissions is called whenever the current user's permissions change
     * @type{function(Object.<string,boolean>): any}
     */
    this.onpermissions = null;
    /**
     * ondownstream is called whenever a new down stream is added.  It
     * should set up the stream's callbacks; actually setting up the UI
     * should be done in the stream's ondowntrack callback.
     * @type{function(Stream): any}
     */
    this.ondownstream = null;
    /**
     * onchat is called whenever a new chat message is received.
     * @type {function(string, string, string, string): any}
     */
    this.onchat = null;
    /**
     * onclearchat is called whenever the server requests that the chat
     * be cleared.
     * @type{function(): any}
     */
    this.onclearchat = null;
    /**
     * onusermessage is called when the server sends an error or warning
     * message that should be displayed to the user.
     * @type{function(string, string): any}
     */
    this.onusermessage = null;
}

/**
  * @typedef {Object} message
  * @property {string} type
  * @property {string} [kind]
  * @property {string} [id]
  * @property {string} [username]
  * @property {string} [password]
  * @property {Object.<string,boolean>} [permissions]
  * @property {string} [group]
  * @property {string} [value]
  * @property {RTCSessionDescriptionInit} [offer]
  * @property {RTCSessionDescriptionInit} [answer]
  * @property {RTCIceCandidate} [candidate]
  * @property {Object.<string,string>} [labels]
  * @property {Object.<string,(boolean|number)>} [request]
  */

/**
 * close forcibly closes a server connection.  The onclose callback will
 * be called when the connection is effectively closed.
 */
ServerConnection.prototype.close = function() {
    this.socket && this.socket.close(1000, 'Close requested by client');
    this.socket = null;
}

/**
  * send sends a message to the server.
  * @param {message} m - the message to send.
  */
ServerConnection.prototype.send = function(m) {
    if(this.socket.readyState !== this.socket.OPEN) {
        // send on a closed connection doesn't throw
        throw(new Error('Connection is not open'));
    }
    return this.socket.send(JSON.stringify(m));
}

/** getIceServers fetches an ICE configuration from the server and
 * populates the iceServers field of a ServerConnection.  It is called
 * lazily by connect.
 *
 * @returns {Promise<Array.<Object>>}
 */
ServerConnection.prototype.getIceServers = async function() {
    let r = await fetch('/ice-servers.json');
    if(!r.ok)
        throw new Error("Couldn't fetch ICE servers: " +
                        r.status + ' ' + r.statusText);
    let servers = await r.json();
    if(!(servers instanceof Array))
        throw new Error("couldn't parse ICE servers");
    this.iceServers = servers;
    return servers;
}

/**
 * Connect connects to the server.
 *
 * @param {string} url - The URL to connect to.
 * @returns {Promise<ServerConnection>}
 */
ServerConnection.prototype.connect = function(url) {
    let sc = this;
    if(sc.socket) {
        sc.socket.close(1000, 'Reconnecting');
        sc.socket = null;
    }

    if(!sc.iceServers) {
        try {
            sc.getIceServers();
        } catch(e) {
            console.error(e);
        }
    }

    try {
        sc.socket = new WebSocket(
            `ws${location.protocol === 'https:' ? 's' : ''}://${location.host}/ws`,
        );
    } catch(e) {
        return Promise.reject(e);
    }

    return new Promise((resolve, reject) => {
        this.socket.onerror = function(e) {
            reject(e);
        };
        this.socket.onopen = function(e) {
            if(sc.onconnected)
                sc.onconnected.call(sc);
            resolve(sc);
        };
        this.socket.onclose = function(e) {
            sc.permissions = {};
            if(sc.onpermissions)
                sc.onpermissions.call(sc, {});
            for(let id in sc.down) {
                let c = sc.down[id];
                delete(sc.down[id]);
                c.close(false);
                if(c.onclose)
                    c.onclose.call(c);
            }
            if(sc.onclose)
                sc.onclose.call(sc, e.code, e.reason);
            reject(new Error('websocket close ' + e.code + ' ' + e.reason));
        };
        this.socket.onmessage = function(e) {
            let m = JSON.parse(e.data);
            switch(m.type) {
            case 'offer':
                sc.gotOffer(m.id, m.labels, m.offer, m.kind === 'renegotiate');
                break;
            case 'answer':
                sc.gotAnswer(m.id, m.answer);
                break;
            case 'renegotiate':
                sc.gotRenegotiate(m.id)
                break;
            case 'close':
                sc.gotClose(m.id);
                break;
            case 'abort':
                sc.gotAbort(m.id);
                break;
            case 'ice':
                sc.gotICE(m.id, m.candidate);
                break;
            case 'label':
                sc.gotLabel(m.id, m.value);
                break;
            case 'permissions':
                sc.permissions = m.permissions;
                if(sc.onpermissions)
                    sc.onpermissions.call(sc, m.permissions);
                break;
            case 'user':
                if(sc.onuser)
                    sc.onuser.call(sc, m.id, m.kind, m.username);
                break;
            case 'chat':
                if(sc.onchat)
                    sc.onchat.call(sc, m.id, m.username, m.kind, m.value);
                break;
            case 'clearchat':
                if(sc.onclearchat)
                    sc.onclearchat.call(sc);
                break;
            case 'ping':
                sc.send({
                    type: 'pong',
                });
                break;
            case 'pong':
                /* nothing */
                break;
            case 'usermessage':
                if(sc.onusermessage)
                    sc.onusermessage.call(sc, m.kind, m.value)
                break;
            default:
                console.warn('Unexpected server message', m.type);
                return;
            }
        };
    });
}

/**
 * login authenticates with the server.
 *
 * @param {string} username
 * @param {string} password
 */
ServerConnection.prototype.login = function(username, password) {
    this.send({
        type: 'login',
        id: this.id,
        username: username,
        password: password,
    })
}

/**
 * join joins a group.
 *
 * @param {string} group - The name of the group to join.
 */
ServerConnection.prototype.join = function(group) {
    this.send({
        type: 'join',
        group: group,
    })
}

/**
 * request sets the list of requested media types.
 *
 * @param {string} what - One of "audio", "screenshare" or "everything".
 */
ServerConnection.prototype.request = function(what) {
    /** @type {Object.<string,boolean>} */
    let request = {};
    switch(what) {
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
        console.error(`Uknown value ${what} in sendRequest`);
        break;
    }

    this.send({
        type: 'request',
        request: request,
    });
}

/**
 * newUpStream requests the creation of a new up stream.
 *
 * @param {string} id - The id of the stream to create (optional).
 * @returns {Stream}
 */
ServerConnection.prototype.newUpStream = function(id) {
    let sc = this;
    if(!id) {
        id = randomid();
        if(sc.up[id])
            throw new Error('Eek!');
    }
    let pc = new RTCPeerConnection({
        iceServers: sc.iceServers,
    });
    if(!pc)
        throw new Error("Couldn't create peer connection");
    if(sc.up[id]) {
        sc.up[id].close(false);
    }
    let c = new Stream(this, id, pc);
    sc.up[id] = c;

    pc.onnegotiationneeded = async e => {
            await c.negotiate();
    }

    pc.onicecandidate = e => {
        if(!e.candidate)
            return;
        sc.send({type: 'ice',
             id: id,
             candidate: e.candidate,
        });
    };

    pc.oniceconnectionstatechange = e => {
        if(c.onstatus)
            c.onstatus.call(c, pc.iceConnectionState);
        if(pc.iceConnectionState === 'failed') {
            try {
                /** @ts-ignore */
                pc.restartIce();
            } catch(e) {
                console.warn(e);
            }
        }
    }

    pc.ontrack = console.error;

    return c;
}

/**
 * chat sends a chat message to the server.  The server will normally echo
 * the message back to the client.
 *
 * @param {string} username - The username of the sending user.
 * @param {string} kind - The kind of message, either "" or "me".
 * @param {string} message - The text of the message.
 */
ServerConnection.prototype.chat = function(username, kind, message) {
    this.send({
        type: 'chat',
        id: this.id,
        username: username,
        kind: kind,
        value: message,
    });
}

/**
 * groupAction sends a request to act on the current group.
 *
 * @param {string} kind - One of "clearchat", "lock", "unlock", "record or
 * "unrecord".
 */
ServerConnection.prototype.groupAction = function(kind) {
    this.send({
        type: 'groupaction',
        kind: kind,
    });
}

/**
 * userAction sends a request to act on a user.
 *
 * @param {string} kind - One of "op", "unop", "kick", "present", "unpresent".
 */
ServerConnection.prototype.userAction = function(kind, id) {
    this.send({
        type: 'useraction',
        kind: kind,
        id: id,
    });
}

/**
 * Called when we receive an offer from the server.  Don't call this.
 *
 * @param {string} id
 * @param labels
 * @param {RTCSessionDescriptionInit} offer
 * @param {boolean} renegotiate
 */
ServerConnection.prototype.gotOffer = async function(id, labels, offer, renegotiate) {
    let sc = this;
    let c = sc.down[id];
    if(c && !renegotiate) {
        // SDP is rather inflexible as to what can be renegotiated.
        // Unless the server indicates that this is a renegotiation with
        // all parameters unchanged, tear down the existing connection.
        delete(sc.down[id])
        c.close(false);
        c = null;
    }

    if(!c) {
        let pc = new RTCPeerConnection({
            iceServers: this.iceServers,
        });
        c = new Stream(this, id, pc);
        sc.down[id] = c;

        c.pc.onicecandidate = function(e) {
            if(!e.candidate)
                return;
            sc.send({type: 'ice',
                  id: id,
                  candidate: e.candidate,
                 });
        };

        pc.oniceconnectionstatechange = e => {
            if(c.onstatus)
                c.onstatus.call(c, pc.iceConnectionState);
            if(pc.iceConnectionState === 'failed') {
                sc.send({type: 'renegotiate',
                         id: id,
                        });
            }
        }

        c.pc.ontrack = function(e) {
            let label = e.transceiver && c.labelsByMid[e.transceiver.mid];
            if(label) {
                c.labels[e.track.id] = label;
            } else {
                console.warn("Couldn't find label for track");
            }
            if(c.stream !== e.streams[0]) {
                c.stream = e.streams[0];
                let label =
                    e.transceiver && c.labelsByMid[e.transceiver.mid];
                c.labels[e.track.id] = label;
                if(c.ondowntrack) {
                    c.ondowntrack.call(
                        c, e.track, e.transceiver, label, e.streams[0],
                    );
                }
                if(c.onlabel) {
                    c.onlabel.call(c, label);
                }
            }
        };
    }

    c.labelsByMid = labels;

    if(sc.ondownstream)
        sc.ondownstream.call(sc, c);

    await c.pc.setRemoteDescription(offer);
    await c.flushIceCandidates();
    let answer = await c.pc.createAnswer();
    if(!answer)
        throw new Error("Didn't create answer");
    await c.pc.setLocalDescription(answer);
    this.send({
        type: 'answer',
        id: id,
        answer: answer,
    });
}

/**
 * Called when we receive a stream label from the server.  Don't call this.
 *
 * @param {string} id
 * @param {string} label
 */
ServerConnection.prototype.gotLabel = function(id, label) {
    let c = this.down[id];
    if(!c)
        throw new Error('Got label for unknown id');

    c.label = label;
    if(c.onlabel)
        c.onlabel.call(c, label);
}

/**
 * Called when we receive an answer from the server.  Don't call this.
 *
 * @param {string} id
 * @param {RTCSessionDescriptionInit} answer
 */
ServerConnection.prototype.gotAnswer = async function(id, answer) {
    let c = this.up[id];
    if(!c)
        throw new Error('unknown up stream');
    try {
        await c.pc.setRemoteDescription(answer);
    } catch(e) {
        if(c.onerror)
            c.onerror.call(c, e);
        return;
    }
    await c.flushIceCandidates();
}

/**
 * Called when we receive a renegotiation request from the server.  Don't
 * call this.
 *
 * @param {string} id
 */
ServerConnection.prototype.gotRenegotiate = async function(id) {
    let c = this.up[id];
    if(!c)
        throw new Error('unknown up stream');
    try {
        /** @ts-ignore */
        c.pc.restartIce();
    } catch(e) {
        console.warn(e);
    }
}

/**
 * Called when we receive a close request from the server.  Don't call this.
 *
 * @param {string} id
 */
ServerConnection.prototype.gotClose = function(id) {
    let c = this.down[id];
    if(!c)
        throw new Error('unknown down stream');
    delete(this.down[id]);
    c.close(false);
    if(c.onclose)
        c.onclose.call(c);
}

/**
 * Called when we receive an abort message from the server.  Don't call this.
 *
 * @param {string} id
 */
ServerConnection.prototype.gotAbort = function(id) {
    let c = this.down[id];
    if(!c)
        throw new Error('unknown up stream');
    if(c.onabort)
        c.onabort.call(c);
}

/**
 * Called when we receive an ICE candidate from the server.  Don't call this.
 *
 * @param {string} id
 * @param {RTCIceCandidate} candidate
 */
ServerConnection.prototype.gotICE = async function(id, candidate) {
    let c = this.up[id];
    if(!c)
        c = this.down[id];
    if(!c)
        throw new Error('unknown stream');
    if(c.pc.remoteDescription)
        await c.pc.addIceCandidate(candidate).catch(console.warn);
    else
        c.iceCandidates.push(candidate);
}

/**
 * Stream encapsulates a MediaStream, a set of tracks.
 *
 * A stream is said to go "up" if it is from the client to the server, and
 * "down" otherwise.
 *
 * @param {ServerConnection} sc
 * @param {string} id
 * @param {RTCPeerConnection} pc
 *
 * @constructor
 */
function Stream(sc, id, pc) {
    /**
     * The associated ServerConnection.
     *
     * @type {ServerConnection}
    */
    this.sc = sc;
    /**
     * The id of this stream.
     *
     * @type {string}
    */
    this.id = id;
    /**
     * For up streams, one of "local" or "screenshare".
     *
     * @type {string}
     */
    this.kind = null;
    /**
     * For down streams, a user-readable label.
     *
     * @type {string}
     */
    this.label = null;
    /**
     * The associated RTCPeerConnectoin.  This is null before the stream
     * is connected, and may change over time.
     *
     * @type {RTCPeerConnection}
     */
    this.pc = pc;
    /**
     * The associated MediaStream.  This is null before the stream is
     * connected, and may change over time.
     *
     * @type {MediaStream}
     */
    this.stream = null;
    /**
     * Track labels, indexed by track id.
     *
     * @type {Object.<string,string>}
     */
    this.labels = {};
    /**
     * Track labels, indexed by mid.
     *
     * @type {Object.<string,string>}
     */
    this.labelsByMid = {};
    /**
     * Buffered ICE candidates.  This will be flushed by flushIceCandidates
     * when the PC becomes stable.
     *
     * @type {Array.<RTCIceCandidate>}
     */
    this.iceCandidates = [];
    /**
     * The statistics last computed by the stats handler.  This is
     * a dictionary indexed by track id, with each value a disctionary of
     * statistics.
     *
     * @type {Object.<string,any>}
     */
    this.stats = {};
    /**
     * The id of the periodic handler that computes statistics, as
     * returned by setInterval.
     *
     * @type {number}
     */
    this.statsHandler = null;

    /* Callbacks */

    /**
     * onclose is called when the stream is closed.
     *
     * @type{function(): any}
     */
    this.onclose = null;
    /**
     * onerror is called whenever an error occurs.  If the error is
     * fatal, then onclose will be called afterwards.
     *
     * @type{function(any): any}
     */
    this.onerror = null;
    /**
     * ondowntrack is called whenever a new track is added to a stream.
     * If the stream parameter differs from its previous value, then it
     * indicates that the old stream has been discarded.
     *
     * @type{function(MediaStreamTrack, RTCRtpTransceiver, string, MediaStream): any}
     */
    this.ondowntrack = null;
    /**
     * onlabel is called whenever the server sets a new label for the stream.
     *
     * @type{function(string): any}
     */
    this.onlabel = null;
    /**
     * onstatus is called whenever the status of the stream changes.
     *
     * @type{function(string): any}
     */
    this.onstatus = null;
    /**
     * onabort is called when the server requested that an up stream be
     * closed.  It is the resposibility of the client to close the stream.
     *
     * @type{function(): any}
     */
    this.onabort = null;
    /**
     * onstats is called when we have new statistics about the connection
     *
     * @type{function(Object.<string,any>): any}
     */
    this.onstats = null;
}

/**
 * close closes an up stream.  It should not be called for down streams.
 * @param {boolean} sendclose - whether to send a close message to the server
 */
Stream.prototype.close = function(sendclose) {
    let c = this;
    if(c.statsHandler) {
        clearInterval(c.statsHandler);
        c.statsHandler = null;
    }

    if(c.stream) {
        c.stream.getTracks().forEach(t => {
            try {
                t.stop();
            } catch(e) {
            }
        });
    }
    c.pc.close();

    if(sendclose) {
        try {
            c.sc.send({
                type: 'close',
                id: c.id,
            });
        } catch(e) {
        }
    }
    c.sc = null;
};

/**
 * flushIceCandidates flushes any buffered ICE candidates.  It is called
 * automatically when the connection reaches a stable state.
 */
Stream.prototype.flushIceCandidates = async function () {
    let promises = [];
    this.iceCandidates.forEach(c => {
        promises.push(this.pc.addIceCandidate(c).catch(console.warn));
    });
    this.iceCandidates = [];
    return await Promise.all(promises);
}

/**
 * negotiate negotiates or renegotiates an up stream.  It is called
 * automatically when required.  If the client requires renegotiation, it
 * is probably more effective to call restartIce on the underlying PC
 * rather than invoking this function directly.
 */
Stream.prototype.negotiate = async function () {
    let c = this;

    let offer = await c.pc.createOffer();
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

    c.sc.send({
        type: 'offer',
        kind: 'renegotiate',
        id: c.id,
        labels: c.labelsByMid,
        offer: offer,
    });
}

/**
 * updateStats is called periodically, if requested by setStatsInterval,
 * in order to recompute stream statistics and invoke the onstats handler.
 *
 * @returns {Promise<void>}
 */
Stream.prototype.updateStats = async function() {
    let c = this;
    let old = c.stats;
    let stats = {};

    let transceivers = c.pc.getTransceivers();
    for(let i = 0; i < transceivers.length; i++) {
        let t = transceivers[i];
        let tid = t.sender.track && t.sender.track.id;
        if(!tid)
            continue;

        let report;
        try {
            report = await t.sender.getStats();
        } catch(e) {
            continue;
        }

        stats[tid] = {};

        for(let r of report.values()) {
            if(r.type !== 'outbound-rtp')
                continue;

            stats[tid].timestamp = r.timestamp;
            stats[tid].bytesSent = r.bytesSent;
            if(old[tid] && old[tid].timestamp) {
                stats[tid].rate =
                    ((r.bytesSent - old[tid].bytesSent) * 1000 /
                     (r.timestamp - old[tid].timestamp)) * 8;
            }
        }
    }

    c.stats = stats;

    if(c.onstats)
        c.onstats.call(c, c.stats);
}

/**
 * setStatsInterval sets the interval in milliseconds at which the onstats
 * handler will be called.  This is only useful for up streams.
 *
 * @param {number} ms
 */
Stream.prototype.setStatsInterval = function(ms) {
    let c = this;
    if(c.statsHandler) {
        clearInterval(c.statsHandler);
        c.statsHandler = null;
    }

    if(ms <= 0)
        return;

    c.statsHandler = setInterval(() => {
        c.updateStats();
    }, ms);
}

