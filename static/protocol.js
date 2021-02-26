// Copyright (c) 2020 by Juliusz Chroboczek.

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

/**
 * toHex formats an array as a hexadecimal string.
 * @param {number[]|Uint8Array} array - the array to format
 * @returns {string} - the hexadecimal representation of array
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

/**
 * newRandomId returns a random string of 32 hex digits (16 bytes).
 *
 * @returns {string}
 */
function newRandomId() {
    let a = new Uint8Array(16);
    crypto.getRandomValues(a);
    return toHex(a);
}

let localIdCounter = 0;

/**
 * newLocalId returns a string that is unique in this session.
 *
 * @returns {string}
 */
function newLocalId() {
    let id = `${localIdCounter}`
    localIdCounter++;
    return id;
}

/**
 * ServerConnection encapsulates a websocket connection to the server and
 * all the associated streams.
 * @constructor
 */
function ServerConnection() {
    /**
     * The id of this connection.
     *
     * @type {string}
     * @const
     */
    this.id = newRandomId();
    /**
     * The group that we have joined, or null if we haven't joined yet.
     *
     * @type {string}
     */
    this.group = null;
    /**
     * The username we joined as.
     */
    this.username = null;
    /**
     * The underlying websocket.
     *
     * @type {WebSocket}
     */
    this.socket = null;
    /**
     * The set of all up streams, indexed by their id.
     *
     * @type {Object<string,Stream>}
     */
    this.up = {};
    /**
     * The set of all down streams, indexed by their id.
     *
     * @type {Object<string,Stream>}
     */
    this.down = {};
    /**
     * The ICE configuration used by all associated streams.
     *
     * @type {RTCConfiguration}
     */
    this.rtcConfiguration = null;
    /**
     * The permissions granted to this connection.
     *
     * @type {Object<string,boolean>}
     */
    this.permissions = {};
    /**
     * userdata is a convenient place to attach data to a ServerConnection.
     * It is not used by the library.
     *
     * @type{Object<unknown,unknown>}
     */
    this.userdata = {};

    /* Callbacks */

    /**
     * onconnected is called when the connection has been established
     *
     * @type{(this: ServerConnection) => void}
     */
    this.onconnected = null;
    /**
     * onclose is called when the connection is closed
     *
     * @type{(this: ServerConnection, code: number, reason: string) => void}
     */
    this.onclose = null;
    /**
     * onuser is called whenever a user is added or removed from the group
     *
     * @type{(this: ServerConnection, id: string, kind: string, username: string) => void}
     */
    this.onuser = null;
    /**
     * onjoined is called whenever we join or leave a group or whenever the
     * permissions we have in a group change.
     *
     * kind is one of 'join', 'fail', 'change' or 'leave'.
     *
     * @type{(this: ServerConnection, kind: string, group: string, permissions: Object<string,boolean>, message: string) => void}
     */
    this.onjoined = null;
    /**
     * ondownstream is called whenever a new down stream is added.  It
     * should set up the stream's callbacks; actually setting up the UI
     * should be done in the stream's ondowntrack callback.
     *
     * @type{(this: ServerConnection, stream: Stream) => void}
     */
    this.ondownstream = null;
    /**
     * onchat is called whenever a new chat message is received.
     *
     * @type {(this: ServerConnection, id: string, dest: string, username: string, time: number, privileged: boolean, kind: string, message: unknown) => void}
     */
    this.onchat = null;
    /**
     * onusermessage is called when an application-specific message is
     * received.  Id is null when the message originated at the server,
     * a user-id otherwise.
     *
     * 'kind' is typically one of 'error', 'warning', 'info' or 'mute'.  If
     * 'id' is non-null, 'privileged' indicates whether the message was
     * sent by an operator.
     *
     * @type {(this: ServerConnection, id: string, dest: string, username: string, time: number, privileged: boolean, kind: string, message: unknown) => void}
     */
    this.onusermessage = null;
}

/**
  * @typedef {Object} message
  * @property {string} type
  * @property {string} [kind]
  * @property {string} [id]
  * @property {string} [replace]
  * @property {string} [source]
  * @property {string} [dest]
  * @property {string} [username]
  * @property {string} [password]
  * @property {boolean} [privileged]
  * @property {Object<string,boolean>} [permissions]
  * @property {string} [group]
  * @property {unknown} [value]
  * @property {boolean} [noecho]
  * @property {string} [sdp]
  * @property {RTCIceCandidate} [candidate]
  * @property {Object<string,string>} [labels]
  * @property {Object<string,(boolean|number)>} [request]
  * @property {Object<string,any>} [rtcConfiguration]
  */

/**
 * close forcibly closes a server connection.  The onclose callback will
 * be called when the connection is effectively closed.
 */
ServerConnection.prototype.close = function() {
    this.socket && this.socket.close(1000, 'Close requested by client');
    this.socket = null;
};

/**
  * send sends a message to the server.
  * @param {message} m - the message to send.
  */
ServerConnection.prototype.send = function(m) {
    if(!this.socket || this.socket.readyState !== this.socket.OPEN) {
        // send on a closed socket doesn't throw
        throw(new Error('Connection is not open'));
    }
    return this.socket.send(JSON.stringify(m));
};

/**
 * connect connects to the server.
 *
 * @param {string} url - The URL to connect to.
 * @returns {Promise<ServerConnection>}
 * @function
 */
ServerConnection.prototype.connect = async function(url) {
    let sc = this;
    if(sc.socket) {
        sc.socket.close(1000, 'Reconnecting');
        sc.socket = null;
    }

    sc.socket = new WebSocket(url);

    return await new Promise((resolve, reject) => {
        this.socket.onerror = function(e) {
            reject(e);
        };
        this.socket.onopen = function(e) {
            sc.send({
                type: 'handshake',
                id: sc.id,
            });
            if(sc.onconnected)
                sc.onconnected.call(sc);
            resolve(sc);
        };
        this.socket.onclose = function(e) {
            sc.permissions = {};
            for(let id in sc.up) {
                let c = sc.up[id];
                c.close();
            }
            for(let id in sc.down) {
                let c = sc.down[id];
                c.close();
            }
            if(sc.group && sc.onjoined)
                sc.onjoined.call(sc, 'leave', sc.group, {}, '');
            sc.group = null;
            sc.username = null;
            if(sc.onclose)
                sc.onclose.call(sc, e.code, e.reason);
            reject(new Error('websocket close ' + e.code + ' ' + e.reason));
        };
        this.socket.onmessage = function(e) {
            let m = JSON.parse(e.data);
            switch(m.type) {
            case 'handshake':
                break;
            case 'offer':
                sc.gotOffer(m.id, m.labels, m.source, m.username,
                            m.sdp, m.replace);
                break;
            case 'answer':
                sc.gotAnswer(m.id, m.sdp);
                break;
            case 'renegotiate':
                sc.gotRenegotiate(m.id);
                break;
            case 'close':
                sc.gotClose(m.id);
                break;
            case 'abort':
                sc.gotAbort(m.id);
                break;
            case 'ice':
                sc.gotRemoteIce(m.id, m.candidate);
                break;
            case 'joined':
                if(sc.group) {
                    if(m.group !== sc.group) {
                        throw new Error('Joined multiple groups');
                    }
                } else {
                    sc.group = m.group;
                }
                sc.username = m.username;
                sc.permissions = m.permissions || [];
                sc.rtcConfiguration = m.rtcConfiguration || null;
                if(sc.onjoined)
                    sc.onjoined.call(sc, m.kind, m.group,
                                     m.permissions || {},
                                     m.value || null);
                break;
            case 'user':
                if(sc.onuser)
                    sc.onuser.call(sc, m.id, m.kind, m.username);
                break;
            case 'chat':
                if(sc.onchat)
                    sc.onchat.call(
                        sc, m.source, m.dest, m.username, m.time,
                        m.privileged, m.kind, m.value,
                    );
                break;
            case 'usermessage':
                if(sc.onusermessage)
                    sc.onusermessage.call(
                        sc, m.source, m.dest, m.username, m.time,
                        m.privileged, m.kind, m.value,
                    );
                break;
            case 'ping':
                sc.send({
                    type: 'pong',
                });
                break;
            case 'pong':
                /* nothing */
                break;
            default:
                console.warn('Unexpected server message', m.type);
                return;
            }
        };
    });
};

/**
 * join requests to join a group.  The onjoined callback will be called
 * when we've effectively joined.
 *
 * @param {string} group - The name of the group to join.
 * @param {string} username - the username to join as.
 * @param {string} password - the password.
 */
ServerConnection.prototype.join = function(group, username, password) {
    this.send({
        type: 'join',
        kind: 'join',
        group: group,
        username: username,
        password: password,
    });
};

/**
 * leave leaves a group.  The onjoined callback will be called when we've
 * effectively left.
 *
 * @param {string} group - The name of the group to join.
 */
ServerConnection.prototype.leave = function(group) {
    this.send({
        type: 'join',
        kind: 'leave',
        group: group,
    });
};

/**
 * request sets the list of requested media types.
 *
 * @param {string} what - One of '', 'audio', 'screenshare' or 'everything'.
 */
ServerConnection.prototype.request = function(what) {
    /** @type {Object<string,boolean>} */
    let request = {};
    switch(what) {
    case '':
        request = {};
        break;
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
        console.error(`Unknown value ${what} in request`);
        break;
    }

    this.send({
        type: 'request',
        request: request,
    });
};

/**
 * @param {string} localId
 * @returns {Stream}
 */

ServerConnection.prototype.findByLocalId = function(localId) {
    if(!localId)
        return null;

    for(let id in serverConnection.up) {
        let s = serverConnection.up[id];
        if(s.localId == localId)
            return s;
    }
    return null;
}

/**
 * newUpStream requests the creation of a new up stream.
 *
 * @param {string} [localId]
 *   - The local id of the stream to create.  If a stream already exists with
 *     the same local id, it is replaced with the new stream.
 * @returns {Stream}
 */
ServerConnection.prototype.newUpStream = function(localId) {
    let sc = this;
    let id = newRandomId();
    if(sc.up[id])
        throw new Error('Eek!');

    if(typeof RTCPeerConnection === 'undefined')
        throw new Error("This browser doesn't support WebRTC");

    let pc = new RTCPeerConnection(sc.rtcConfiguration);
    if(!pc)
        throw new Error("Couldn't create peer connection");

    let oldId = null;
    if(localId) {
        let old = sc.findByLocalId(localId);
        oldId = old && old.id;
        if(old)
            old.close(true);
    }

    let c = new Stream(this, id, localId || newLocalId(), pc, true);
    if(oldId)
        c.replace = oldId;
    sc.up[id] = c;

    pc.onnegotiationneeded = async e => {
            await c.negotiate();
    };

    pc.onicecandidate = e => {
        if(!e.candidate)
            return;
        c.gotLocalIce(e.candidate);
    };

    pc.oniceconnectionstatechange = e => {
        if(c.onstatus)
            c.onstatus.call(c, pc.iceConnectionState);
        if(pc.iceConnectionState === 'failed')
            c.restartIce();
    };

    pc.ontrack = console.error;

    return c;
};

/**
 * chat sends a chat message to the server.  The server will normally echo
 * the message back to the client.
 *
 * @param {string} kind
 *     -  The kind of message, either '', 'me' or an application-specific type.
 * @param {string} dest - The id to send the message to, empty for broadcast.
 * @param {string} value - The text of the message.
 */
ServerConnection.prototype.chat = function(kind, dest, value) {
    this.send({
        type: 'chat',
        source: this.id,
        dest: dest,
        username: this.username,
        kind: kind,
        value: value,
    });
};

/**
 * userAction sends a request to act on a user.
 *
 * @param {string} kind - One of "op", "unop", "kick", "present", "unpresent".
 * @param {string} dest - The id of the user to act upon.
 * @param {string} [value] - An optional user-readable message.
 */
ServerConnection.prototype.userAction = function(kind, dest, value) {
    this.send({
        type: 'useraction',
        source: this.id,
        dest: dest,
        username: this.username,
        kind: kind,
        value: value,
    });
};

/**
 * userMessage sends an application-specific message to a user.
 * This is similar to a chat message, but is not saved in the chat history.
 *
 * @param {string} kind - The kind of application-specific message.
 * @param {string} dest - The id to send the message to, empty for broadcast.
 * @param {unknown} [value] - An optional parameter.
 * @param {boolean} [noecho] - If set, don't echo back the message to the sender.
 */
ServerConnection.prototype.userMessage = function(kind, dest, value, noecho) {
    this.send({
        type: 'usermessage',
        source: this.id,
        dest: dest,
        username: this.username,
        kind: kind,
        value: value,
        noecho: noecho,
    });
};

/**
 * groupAction sends a request to act on the current group.
 *
 * @param {string} kind
 *     - One of 'clearchat', 'lock', 'unlock', 'record' or 'unrecord'.
 * @param {string} [message] - An optional user-readable message.
 */
ServerConnection.prototype.groupAction = function(kind, message) {
    this.send({
        type: 'groupaction',
        source: this.id,
        kind: kind,
        username: this.username,
        value: message,
    });
};

/**
 * Called when we receive an offer from the server.  Don't call this.
 *
 * @param {string} id
 * @param {Object<string, string>} labels
 * @param {string} source
 * @param {string} username
 * @param {string} sdp
 * @param {string} replace
 * @function
 */
ServerConnection.prototype.gotOffer = async function(id, labels, source, username, sdp, replace) {
    let sc = this;

    if(sc.up[id]) {
        console.error("Duplicate connection id");
        sc.send({
            type: 'abort',
            id: id,
        });
        return;
    }

    let oldLocalId = null;

    if(replace) {
        let old = sc.down[replace];
        if(old) {
            oldLocalId = old.localId;
            old.close(true);
        } else
            console.error("Replacing unknown stream");
    }

    let c = sc.down[id];
    if(c && oldLocalId)
        console.error("Replacing duplicate stream");

    if(!c) {
        let pc;
        try {
            pc = new RTCPeerConnection(sc.rtcConfiguration);
        } catch(e) {
            console.error(e);
            sc.send({
                type: 'abort',
                id: id,
            });
            return;
        }
        c = new Stream(this, id, oldLocalId || newLocalId(), pc, false);
        sc.down[id] = c;

        c.pc.onicecandidate = function(e) {
            if(!e.candidate)
                return;
            c.gotLocalIce(e.candidate);
        };

        pc.oniceconnectionstatechange = e => {
            if(c.onstatus)
                c.onstatus.call(c, pc.iceConnectionState);
            if(pc.iceConnectionState === 'failed') {
                sc.send({
                    type: 'renegotiate',
                    id: id,
                });
            }
        };

        c.pc.ontrack = function(e) {
            let label = e.transceiver && c.labelsByMid[e.transceiver.mid];
            if(label) {
                c.labels[e.track.id] = label;
            } else {
                console.warn("Couldn't find label for track");
            }
            c.stream = e.streams[0];
            if(c.ondowntrack) {
                c.ondowntrack.call(
                    c, e.track, e.transceiver, label, e.streams[0],
                );
            }
        };
    }

    c.labelsByMid = labels;
    c.labels = {};
    c.pc.getTransceivers().forEach(transceiver => {
        let label = c.labelsByMid[transceiver.mid];
        let track = transceiver.receiver && transceiver.receiver.track;
        if(label && track) {
            c.labels[track.id] = label;
        } else if(!track) {
            console.warn("Couldn't find track for label");
        }
    });
    c.source = source;
    c.username = username;

    if(sc.ondownstream)
        sc.ondownstream.call(sc, c);

    try {
        await c.pc.setRemoteDescription({
            type: 'offer',
            sdp: sdp,
        });

        await c.flushRemoteIceCandidates();

        let answer = await c.pc.createAnswer();
        if(!answer)
            throw new Error("Didn't create answer");
        await c.pc.setLocalDescription(answer);
        this.send({
            type: 'answer',
            id: id,
            sdp: c.pc.localDescription.sdp,
        });
    } catch(e) {
        try {
            if(c.onerror)
                c.onerror.call(c, e);
        } finally {
            c.abort();
        }
        return;
    }

    c.localDescriptionSent = true;
    c.flushLocalIceCandidates();
    if(c.onnegotiationcompleted)
        c.onnegotiationcompleted.call(c);
};

/**
 * Called when we receive an answer from the server.  Don't call this.
 *
 * @param {string} id
 * @param {string} sdp
 * @function
 */
ServerConnection.prototype.gotAnswer = async function(id, sdp) {
    let c = this.up[id];
    if(!c)
        throw new Error('unknown up stream');
    try {
        await c.pc.setRemoteDescription({
            type: 'answer',
            sdp: sdp,
        });
    } catch(e) {
        try {
            if(c.onerror)
                c.onerror.call(c, e);
        } finally {
            c.close();
        }
        return;
    }
    await c.flushRemoteIceCandidates();
    if(c.onnegotiationcompleted)
        c.onnegotiationcompleted.call(c);
};

/**
 * Called when we receive a renegotiation request from the server.  Don't
 * call this.
 *
 * @param {string} id
 * @function
 */
ServerConnection.prototype.gotRenegotiate = async function(id) {
    let c = this.up[id];
    if(!c)
        throw new Error('unknown up stream');
    c.restartIce();
};

/**
 * Called when we receive a close request from the server.  Don't call this.
 *
 * @param {string} id
 */
ServerConnection.prototype.gotClose = function(id) {
    let c = this.down[id];
    if(!c)
        throw new Error('unknown down stream');
    c.close();
};

/**
 * Called when we receive an abort message from the server.  Don't call this.
 *
 * @param {string} id
 */
ServerConnection.prototype.gotAbort = function(id) {
    let c = this.up[id];
    if(!c)
        throw new Error('unknown up stream');
    if(c.onabort)
        c.onabort.call(c);
};

/**
 * Called when we receive an ICE candidate from the server.  Don't call this.
 *
 * @param {string} id
 * @param {RTCIceCandidate} candidate
 * @function
 */
ServerConnection.prototype.gotRemoteIce = async function(id, candidate) {
    let c = this.up[id];
    if(!c)
        c = this.down[id];
    if(!c)
        throw new Error('unknown stream');
    if(c.pc.remoteDescription)
        await c.pc.addIceCandidate(candidate).catch(console.warn);
    else
        c.remoteIceCandidates.push(candidate);
};

/**
 * Stream encapsulates a MediaStream, a set of tracks.
 *
 * A stream is said to go "up" if it is from the client to the server, and
 * "down" otherwise.
 *
 * @param {ServerConnection} sc
 * @param {string} id
 * @param {string} localId
 * @param {RTCPeerConnection} pc
 *
 * @constructor
 */
function Stream(sc, id, localId, pc, up) {
    /**
     * The associated ServerConnection.
     *
     * @type {ServerConnection}
     * @const
     */
    this.sc = sc;
    /**
     * The id of this stream.
     *
     * @type {string}
     * @const
     */
    this.id = id;
    /**
     * The local id of this stream.
     *
     * @type {string}
     * @const
     */
    this.localId = localId;
    /**
     * Indicates whether the stream is in the client->server direction.
     *
     * @type {boolean}
     * @const
     */
    this.up = up;
    /**
     * For up streams, one of "local" or "screenshare".
     *
     * @type {string}
     */
    this.kind = null;
    /**
     * For down streams, the id of the client that created the stream.
     *
     * @type {string}
     */
    this.source = null;
    /**
     * For down streams, the username of the client who created the stream.
     *
     * @type {string}
     */
    this.username = null;
    /**
     * The associated RTCPeerConnection.  This is null before the stream
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
     * @type {Object<string,string>}
     */
    this.labels = {};
    /**
     * Track labels, indexed by mid.
     *
     * @type {Object<string,string>}
     */
    this.labelsByMid = {};
    /**
     * The id of the stream that we are currently replacing.
     *
     * @type {string}
     */
    this.replace = null;
    /**
     * Indicates whether we have already sent a local description.
     *
     * @type {boolean}
     */
    this.localDescriptionSent = false;
    /**
     * Buffered local ICE candidates.  This will be flushed by
     * flushLocalIceCandidates after we send a local description.
     *
     * @type {RTCIceCandidate[]}
     */
    this.localIceCandidates = [];
    /**
     * Buffered remote ICE candidates.  This will be flushed by
     * flushRemoteIceCandidates when we get a remote SDP description.
     *
     * @type {RTCIceCandidate[]}
     */
    this.remoteIceCandidates = [];
    /**
     * The statistics last computed by the stats handler.  This is
     * a dictionary indexed by track id, with each value a dictionary of
     * statistics.
     *
     * @type {Object<string,unknown>}
     */
    this.stats = {};
    /**
     * The id of the periodic handler that computes statistics, as
     * returned by setInterval.
     *
     * @type {number}
     */
    this.statsHandler = null;
    /**
     * userdata is a convenient place to attach data to a Stream.
     * It is not used by the library.
     *
     * @type{Object<unknown,unknown>}
     */
    this.userdata = {};

    /* Callbacks */

    /**
     * onclose is called when the stream is closed.  Replace will be true
     * if the stream is being replaced by another one with the same id.
     *
     * @type{(this: Stream, replace: boolean) => void}
     */
    this.onclose = null;
    /**
     * onerror is called whenever an error occurs.  If the error is
     * fatal, then onclose will be called afterwards.
     *
     * @type{(this: Stream, error: unknown) => void}
     */
    this.onerror = null;
    /**
     * onnegotiationcompleted is called whenever negotiation or
     * renegotiation has completed.
     *
     * @type{(this: Stream) => void}
     */
    this.onnegotiationcompleted = null;
    /**
     * ondowntrack is called whenever a new track is added to a stream.
     * If the stream parameter differs from its previous value, then it
     * indicates that the old stream has been discarded.
     *
     * @type{(this: Stream, track: MediaStreamTrack, transceiver: RTCRtpTransceiver, label: string, stream: MediaStream) => void}
     */
    this.ondowntrack = null;
    /**
     * onstatus is called whenever the status of the stream changes.
     *
     * @type{(this: Stream, status: string) => void}
     */
    this.onstatus = null;
    /**
     * onabort is called when the server requested that an up stream be
     * closed.  It is the resposibility of the client to close the stream.
     *
     * @type{(this: Stream) => void}
     */
    this.onabort = null;
    /**
     * onstats is called when we have new statistics about the connection
     *
     * @type{(this: Stream, stats: Object<unknown,unknown>) => void}
     */
    this.onstats = null;
}

/**
 * close closes a stream.
 *
 * For streams in the up direction, this may be called at any time.  For
 * streams in the down direction, this will be called automatically when
 * the server signals that it is closing a stream.
 *
 * @param {boolean} [replace]
 *    - true if the stream is being replaced by another one with the same id
 */
Stream.prototype.close = function(replace) {
    let c = this;

    if(!c.sc) {
        console.warn('Closing closed stream');
        return;
    }

    if(c.statsHandler) {
        clearInterval(c.statsHandler);
        c.statsHandler = null;
    }

    c.pc.close();

    if(c.up && !replace && c.localDescriptionSent) {
        try {
            c.sc.send({
                type: 'close',
                id: c.id,
            });
        } catch(e) {
        }
    }

    if(c.up) {
        if(c.sc.up[c.id] === c)
            delete(c.sc.up[c.id]);
        else
            console.warn('Closing unknown stream');
    } else {
        if(c.sc.down[c.id] === c)
            delete(c.sc.down[c.id]);
        else
            console.warn('Closing unknown stream');
    }
    c.sc = null;

    if(c.onclose)
        c.onclose.call(c, replace);
};

/**
 * abort requests that the server close a down stream.
 */
Stream.prototype.abort = function() {
    let c = this;
    if(c.up)
        throw new Error("Abort called on an up stream");
    c.sc.send({
        type: 'abort',
        id: c.id,
    });
};

/**
 * Called when we get a local ICE candidate.  Don't call this.
 *
 * @param {RTCIceCandidate} candidate
 * @function
 */
Stream.prototype.gotLocalIce = function(candidate) {
    let c = this;
    if(c.localDescriptionSent)
        c.sc.send({type: 'ice',
                   id: c.id,
                   candidate: candidate,
                  });
    else
        c.localIceCandidates.push(candidate);
};

/**
 * flushLocalIceCandidates flushes any buffered local ICE candidates.
 * It is called when we send an offer.
 * @function
 */
Stream.prototype.flushLocalIceCandidates = function () {
    let c = this;
    let candidates = c.localIceCandidates;
    c.localIceCandidates = [];
    candidates.forEach(candidate => {
        try {
            c.sc.send({type: 'ice',
                       id: c.id,
                       candidate: candidate,
                      });
        } catch(e) {
            console.warn(e);
        }
    });
    c.localIceCandidates = [];
};

/**
 * flushRemoteIceCandidates flushes any buffered remote ICE candidates.  It is
 * called automatically when we get a remote description.
 * @function
 */
Stream.prototype.flushRemoteIceCandidates = async function () {
    let c = this;
    let candidates = c.remoteIceCandidates;
    c.remoteIceCandidates = [];
    /** @type {Array.<Promise<void>>} */
    let promises = [];
    candidates.forEach(candidate => {
        promises.push(c.pc.addIceCandidate(candidate).catch(console.warn));
    });
    return await Promise.all(promises);
};

/**
 * negotiate negotiates or renegotiates an up stream.  It is called
 * automatically when required.  If the client requires renegotiation, it
 * is probably better to call restartIce which will cause negotiate to be
 * called asynchronously.
 *
 * @function
 * @param {boolean} [restartIce] - Whether to restart ICE.
 */
Stream.prototype.negotiate = async function (restartIce) {
    let c = this;
    if(!c.up)
        throw new Error('not an up stream');

    let options = {};
    if(restartIce)
        options = {iceRestart: true};
    let offer = await c.pc.createOffer(options);
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
        source: c.sc.id,
        username: c.sc.username,
        kind: this.localDescriptionSent ? 'renegotiate' : '',
        id: c.id,
        replace: this.replace,
        labels: c.labelsByMid,
        sdp: c.pc.localDescription.sdp,
    });
    this.localDescriptionSent = true;
    this.replace = null;
    c.flushLocalIceCandidates();
};

/**
 * restartIce causes an ICE restart on a stream.  For up streams, it is
 * called automatically when ICE signals that the connection has failed,
 * but may also be called by the application.  For down streams, it
 * requests that the server perform an ICE restart.  In either case,
 * it returns immediately, negotiation will happen asynchronously.
 */

Stream.prototype.restartIce = function () {
    let c = this;
    if(!c.up) {
        c.sc.send({
            type: 'renegotiate',
            id: c.id,
        });
        return;
    }

    if('restartIce' in c.pc) {
        try {
            /** @ts-ignore */
            c.pc.restartIce();
            return;
        } catch(e) {
            console.warn(e);
        }
    }

    // negotiate is async, but this returns immediately.
    c.negotiate(true);
};

/**
 * updateStats is called periodically, if requested by setStatsInterval,
 * in order to recompute stream statistics and invoke the onstats handler.
 *
 * @function
 */
Stream.prototype.updateStats = async function() {
    let c = this;
    let old = c.stats;
    /** @type{Object<string,unknown>} */
    let stats = {};

    let transceivers = c.pc.getTransceivers();
    for(let i = 0; i < transceivers.length; i++) {
        let t = transceivers[i];
        let stid = t.sender.track && t.sender.track.id;
        let rtid = t.receiver.track && t.receiver.track.id;

        let report = null;
        if(stid) {
            try {
                report = await t.sender.getStats();
            } catch(e) {
            }
        }

        if(report) {
            for(let r of report.values()) {
                if(stid && r.type === 'outbound-rtp') {
                    if(!('bytesSent' in r))
                        continue;
                    if(!stats[stid])
                        stats[stid] = {};
                    stats[stid][r.type] = {};
                    stats[stid][r.type].timestamp = r.timestamp;
                    stats[stid][r.type].bytesSent = r.bytesSent;
                    if(old[stid] && old[stid][r.type])
                        stats[stid][r.type].rate =
                        ((r.bytesSent - old[stid][r.type].bytesSent) * 1000 /
                         (r.timestamp - old[stid][r.type].timestamp)) * 8;
                }
            }
        }

        report = null;
        if(rtid) {
            try {
                report = await t.receiver.getStats();
            } catch(e) {
                console.error(e);
            }
        }

        if(report) {
            for(let r of report.values()) {
                if(rtid && r.type === 'track') {
                    if(!('totalAudioEnergy' in r))
                        continue;
                    if(!stats[rtid])
                        stats[rtid] = {};
                    stats[rtid][r.type] = {};
                    stats[rtid][r.type].timestamp = r.timestamp;
                    stats[rtid][r.type].totalAudioEnergy = r.totalAudioEnergy;
                    if(old[rtid] && old[rtid][r.type])
                        stats[rtid][r.type].audioEnergy =
                        (r.totalAudioEnergy - old[rtid][r.type].totalAudioEnergy) * 1000 /
                        (r.timestamp - old[rtid][r.type].timestamp);
                }
            }
        }
    }

    c.stats = stats;

    if(c.onstats)
        c.onstats.call(c, c.stats);
};

/**
 * setStatsInterval sets the interval in milliseconds at which the onstats
 * handler will be called.  This is only useful for up streams.
 *
 * @param {number} ms - The interval in milliseconds.
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
};
