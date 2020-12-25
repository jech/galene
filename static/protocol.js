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
     *
     * @type {string}
     * @const
     */
    this.id = randomid();
    /**
     * The group that we have joined, or nil if we haven't joined yet.
     *
     * @type {string}
     */
    this.group = null;
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
     * @type {RTCIceServer[]}
     */
    this.iceServers = null;
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
     * @type {(this: ServerConnection, id: string, dest: string, username: string, time: number, privileged: boolean, kind: string, message: string) => void}
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
     * @type {(this: ServerConnection, id: string, dest: string, username: string, time: number, privileged: boolean, kind: string, message: string) => void}
     */
    this.onusermessage = null;
    /**
     * onclearchat is called whenever the server requests that the chat
     * be cleared.
     *
     * @type{(this: ServerConnection) => void}
     */
    this.onclearchat = null;
}

/**
  * @typedef {Object} message
  * @property {string} type
  * @property {string} [kind]
  * @property {string} [id]
  * @property {string} [dest]
  * @property {string} [username]
  * @property {string} [password]
  * @property {boolean} [privileged]
  * @property {Object<string,boolean>} [permissions]
  * @property {string} [group]
  * @property {string} [value]
  * @property {RTCSessionDescriptionInit} [offer]
  * @property {RTCSessionDescriptionInit} [answer]
  * @property {RTCIceCandidate} [candidate]
  * @property {Object<string,string>} [labels]
  * @property {Object<string,(boolean|number)>} [request]
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
    if(!this.socket || this.socket.readyState !== this.socket.OPEN) {
        // send on a closed socket doesn't throw
        throw(new Error('Connection is not open'));
    }
    return this.socket.send(JSON.stringify(m));
}

/**
 * getIceServers fetches an ICE configuration from the server and
 * populates the iceServers field of a ServerConnection.  It is called
 * lazily by connect.
 *
 * @returns {Promise<RTCIceServer[]>}
 * @function
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

    if(!sc.iceServers) {
        try {
            await sc.getIceServers();
        } catch(e) {
            console.warn(e);
        }
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
            for(let id in sc.down) {
                let c = sc.down[id];
                delete(sc.down[id]);
                c.close();
                if(c.onclose)
                    c.onclose.call(c);
            }
            if(sc.group && sc.onjoined)
                sc.onjoined.call(sc, 'leave', sc.group, {}, '');
            sc.group = null;
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
                sc.gotRemoteIce(m.id, m.candidate);
                break;
            case 'label':
                sc.gotLabel(m.id, m.value);
                break;
            case 'joined':
                if(sc.group) {
                    if(m.group !== sc.group) {
                        throw new Error('Joined multiple groups');
                    }
                } else {
                    sc.group = m.group;
                }
                sc.permissions = m.permissions;
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
                        sc, m.id, m.dest, m.username, m.time,
                        m.privileged, m.kind, m.value,
                    );
                break;
            case 'usermessage':
                if(sc.onusermessage)
                    sc.onusermessage.call(
                        sc, m.id, m.dest, m.username, m.time,
                        m.privileged, m.kind, m.value,
                    );
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
            default:
                console.warn('Unexpected server message', m.type);
                return;
            }
        };
    });
}

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
}

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
}

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
 * newUpStream requests the creation of a new up stream.
 *
 * @param {string} [id] - The id of the stream to create.
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
        iceServers: sc.iceServers || [],
    });
    if(!pc)
        throw new Error("Couldn't create peer connection");
    if(sc.up[id]) {
        sc.up[id].close();
    }
    let c = new Stream(this, id, pc, true);
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
}

/**
 * chat sends a chat message to the server.  The server will normally echo
 * the message back to the client.
 *
 * @param {string} username - The sender's username.
 * @param {string} kind
 *     -  The kind of message, either '', 'me' or an application-specific type.
 * @param {string} dest - The id to send the message to, empty for broadcast.
 * @param {string} value - The text of the message.
 */
ServerConnection.prototype.chat = function(username, kind, dest, value) {
    this.send({
        type: 'chat',
        id: this.id,
        dest: dest,
        username: username,
        kind: kind,
        value: value,
    });
};

/**
 * userAction sends a request to act on a user.
 *
 * @param {string} username - The sender's username.
 * @param {string} kind - One of "op", "unop", "kick", "present", "unpresent".
 * @param {string} dest - The id of the user to act upon.
 * @param {string} [value] - An optional user-readable message.
 */
ServerConnection.prototype.userAction = function(username, kind, dest, value) {
    this.send({
        type: 'useraction',
        id: this.id,
        dest: dest,
        username: username,
        kind: kind,
        value: value,
    });
};

/**
 * userMessage sends an application-specific message to a user.
 * This is similar to a chat message, but is not saved in the chat history.
 *
 * @param {string} username - The sender's username.
 * @param {string} kind - The kind of application-specific message.
 * @param {string} dest - The id to send the message to, empty for broadcast.
 * @param {string} [value] - An optional parameter.
 */
ServerConnection.prototype.userMessage = function(username, kind, dest, value) {
    this.send({
        type: 'usermessage',
        id: this.id,
        dest: dest,
        username: username,
        kind: kind,
        value: value,
    });
};

/**
 * groupAction sends a request to act on the current group.
 *
 * @param {string} username - The sender's username.
 * @param {string} kind
 *     - One of 'clearchat', 'lock', 'unlock', 'record' or 'unrecord'.
 * @param {string} [message] - An optional user-readable message.
 */
ServerConnection.prototype.groupAction = function(username, kind, message) {
    this.send({
        type: 'groupaction',
        id: this.id,
        kind: kind,
        username: username,
        value: message,
    });
};

/**
 * Called when we receive an offer from the server.  Don't call this.
 *
 * @param {string} id
 * @param {Object<string, string>} labels
 * @param {RTCSessionDescriptionInit} offer
 * @param {boolean} renegotiate
 * @function
 */
ServerConnection.prototype.gotOffer = async function(id, labels, offer, renegotiate) {
    let sc = this;
    let c = sc.down[id];
    if(c && !renegotiate) {
        // SDP is rather inflexible as to what can be renegotiated.
        // Unless the server indicates that this is a renegotiation with
        // all parameters unchanged, tear down the existing connection.
        delete(sc.down[id]);
        c.close();
        c = null;
    }

    if(sc.up[id])
        throw new Error('Duplicate connection id');

    if(!c) {
        let pc = new RTCPeerConnection({
            iceServers: this.iceServers,
        });
        c = new Stream(this, id, pc, false);
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
    await c.flushRemoteIceCandidates()
    let answer = await c.pc.createAnswer();
    if(!answer)
        throw new Error("Didn't create answer");
    await c.pc.setLocalDescription(answer);
    this.send({
        type: 'answer',
        id: id,
        answer: answer,
    });
    c.localDescriptionSent = true;
    c.flushLocalIceCandidates();
    if(c.onnegotiationcompleted)
        c.onnegotiationcompleted.call(c);
};

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
};

/**
 * Called when we receive an answer from the server.  Don't call this.
 *
 * @param {string} id
 * @param {RTCSessionDescriptionInit} answer
 * @function
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
    delete(this.down[id]);
    c.close();
    if(c.onclose)
        c.onclose.call(c);
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
 * @param {RTCPeerConnection} pc
 *
 * @constructor
 */
function Stream(sc, id, pc, up) {
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
     * Indicates whether the stream is in the client->server direction.
     *
     * @type {boolean}
     * @const
     */
     this.up = up
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
     * onclose is called when the stream is closed.
     *
     * @type{(this: Stream) => void}
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
     * onlabel is called whenever the server sets a new label for the stream.
     *
     * @type{(this: Stream, label: string) => void}
     */
    this.onlabel = null;
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
 */
Stream.prototype.close = function() {
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

    if(c.up && c.localDescriptionSent) {
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
}

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
}

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
}

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
        kind: this.localDescriptionSent ? 'renegotiate' : '',
        id: c.id,
        labels: c.labelsByMid,
        offer: offer,
    });
    this.localDescriptionSent = true;
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
