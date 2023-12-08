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
 *
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
 * @typedef {Object} user
 * @property {string} username
 * @property {Array<string>} permissions
 * @property {Object<string,any>} data
 * @property {Object<string,Object<string,boolean>>} streams
 */

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
     *
     * @type {string}
     */
    this.username = null;
    /**
     * The set of users in this group, including ourself.
     *
     * @type {Object<string,user>}
     */
    this.users = {};
    /**
     * The underlying websocket.
     *
     * @type {WebSocket}
     */
    this.socket = null;
    /**
     * The negotiated protocol version.
     *
     * @type {string}
     */
    this.version = null;
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
     * @type {Array<string>}
     */
    this.permissions = [];
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
     * onpeerconnection is called before we establish a new peer connection.
     * It may either return null, or a new RTCConfiguration that overrides
     * the value obtained from the server.
     *
     * @type{(this: ServerConnection) => RTCConfiguration}
     */
    this.onpeerconnection = null;
    /**
     * onuser is called whenever a user in the group changes.  The users
     * array has already been updated.
     *
     * @type{(this: ServerConnection, id: string, kind: string) => void}
     */
    this.onuser = null;
    /**
     * onjoined is called whenever we join or leave a group or whenever the
     * permissions we have in a group change.
     *
     * kind is one of 'join', 'fail', 'change' or 'leave'.
     *
     * @type{(this: ServerConnection, kind: string, group: string, permissions: Array<string>, status: Object<string,any>, data: Object<string,any>, error: string, message: string) => void}
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
     * @type {(this: ServerConnection, id: string, dest: string, username: string, time: Date, privileged: boolean, history: boolean, kind: string, message: string) => void}
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
     * @type {(this: ServerConnection, id: string, dest: string, username: string, time: Date, privileged: boolean, kind: string, error: string, message: unknown) => void}
     */
    this.onusermessage = null;
    /**
     * The set of files currently being transferred.
     *
     * @type {Object<string,TransferredFile>}
    */
    this.transferredFiles = {};
    /**
     * onfiletransfer is called whenever a peer offers a file transfer.
     *
     * If the transfer is accepted, it should set up the file transfer
     * callbacks and return immediately.  It may also throw an exception
     * in order to reject the file transfer.
     *
     * @type {(this: ServerConnection, f: TransferredFile) => void}
     */
    this.onfiletransfer = null;
}

/**
  * @typedef {Object} message
  * @property {string} type
  * @property {Array<string>} [version]
  * @property {string} [kind]
  * @property {string} [error]
  * @property {string} [id]
  * @property {string} [replace]
  * @property {string} [source]
  * @property {string} [dest]
  * @property {string} [username]
  * @property {string} [password]
  * @property {string} [token]
  * @property {boolean} [privileged]
  * @property {Array<string>} [permissions]
  * @property {Object<string,any>} [status]
  * @property {Object<string,any>} [data]
  * @property {string} [group]
  * @property {unknown} [value]
  * @property {boolean} [noecho]
  * @property {string|number} [time]
  * @property {string} [sdp]
  * @property {RTCIceCandidate} [candidate]
  * @property {string} [label]
  * @property {Object<string,Array<string>>|Array<string>} [request]
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
                version: ['2'],
                id: sc.id,
            });
            if(sc.onconnected)
                sc.onconnected.call(sc);
            resolve(sc);
        };
        this.socket.onclose = function(e) {
            sc.permissions = [];
            for(let id in sc.up) {
                let c = sc.up[id];
                c.close();
            }
            for(let id in sc.down) {
                let c = sc.down[id];
                c.close();
            }
            for(let id in sc.users) {
                delete(sc.users[id]);
                if(sc.onuser)
                    sc.onuser.call(sc, id, 'delete');
            }
            if(sc.group && sc.onjoined)
                sc.onjoined.call(sc, 'leave', sc.group, [], {}, {}, '', '');
            sc.group = null;
            sc.username = null;
            if(sc.onclose)
                sc.onclose.call(sc, e.code, e.reason);
            reject(new Error('websocket close ' + e.code + ' ' + e.reason));
        };
        this.socket.onmessage = function(e) {
            let m = JSON.parse(e.data);
            switch(m.type) {
            case 'handshake': {
                if((m.version instanceof Array) && m.version.includes('2')) {
                    sc.version = '2';
                } else {
                    sc.version = null;
                    console.error(`Unknown protocol version ${m.version}`);
                    throw new Error(`Unknown protocol version ${m.version}`);
                }
                break;
            }
            case 'offer':
                sc.gotOffer(m.id, m.label, m.source, m.username,
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
                if(m.kind === 'leave' || m.kind === 'fail') {
                    for(let id in sc.users) {
                        delete(sc.users[id]);
                        if(sc.onuser)
                            sc.onuser.call(sc, id, 'delete');
                    }
                    sc.username = null;
                    sc.permissions = [];
                    sc.rtcConfiguration = null;
                } else if(m.kind === 'join' || m.kind == 'change') {
                    if(m.kind === 'join' && sc.group) {
                        throw new Error('Joined multiple groups');
                    } else if(m.kind === 'change' && m.group != sc.group) {
                        console.warn('join(change) for inconsistent group');
                        break;
                    }
                    sc.group = m.group;
                    sc.username = m.username;
                    sc.permissions = m.permissions || [];
                    sc.rtcConfiguration = m.rtcConfiguration || null;
                }
                if(sc.onjoined)
                    sc.onjoined.call(sc, m.kind, m.group,
                                     m.permissions || [],
                                     m.status, m.data,
                                     m.error || null, m.value || null);
                break;
            case 'user':
                switch(m.kind) {
                case 'add':
                    if(m.id in sc.users)
                        console.warn(`Duplicate user ${m.id} ${m.username}`);
                    sc.users[m.id] = {
                        username: m.username,
                        permissions: m.permissions || [],
                        data: m.data || {},
                        streams: {},
                    };
                    break;
                case 'change':
                    if(!(m.id in sc.users)) {
                        console.warn(`Unknown user ${m.id} ${m.username}`);
                        sc.users[m.id] = {
                            username: m.username,
                            permissions: m.permissions || [],
                            data: m.data || {},
                            streams: {},
                        };
                    } else {
                        sc.users[m.id].username = m.username;
                        sc.users[m.id].permissions = m.permissions || [];
                        sc.users[m.id].data = m.data || {};
                    }
                    break;
                case 'delete':
                    if(!(m.id in sc.users))
                        console.warn(`Unknown user ${m.id} ${m.username}`);
                    for(let t in sc.transferredFiles) {
                        let f = sc.transferredFiles[t];
                        if(f.userid === m.id)
                            f.fail('user has gone away');
                    }
                    delete(sc.users[m.id]);
                    break;
                default:
                    console.warn(`Unknown user action ${m.kind}`);
                    return;
                }
                if(sc.onuser)
                    sc.onuser.call(sc, m.id, m.kind);
                break;
            case 'chat':
            case 'chathistory':
                if(sc.onchat)
                    sc.onchat.call(
                        sc, m.source, m.dest, m.username, parseTime(m.time),
                        m.privileged, m.type === 'chathistory', m.kind,
                        '' + m.value,
                    );
                break;
            case 'usermessage':
                if(m.kind === 'filetransfer')
                    sc.fileTransfer(m.source, m.username, m.value);
                else if(sc.onusermessage)
                    sc.onusermessage.call(
                        sc, m.source, m.dest, m.username, parseTime(m.time),
                        m.privileged, m.kind, m.error, m.value,
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
 * Protocol version 1 uses integers for dates, later versions use dates in
 * ISO 8601 format.  This function takes a date in either format and
 * returns a Date object.
 *
 * @param {string|number} value
 * @returns {Date}
 */
function parseTime(value) {
    if(!value)
        return null;
    try {
        return new Date(value);
    } catch(e) {
        console.warn(`Couldn't parse ${value}:`, e);
        return null;
    }
}

/**
 * join requests to join a group.  The onjoined callback will be called
 * when we've effectively joined.
 *
 * @param {string} group - The name of the group to join.
 * @param {string} username - the username to join as.
 * @param {string|Object} credentials - password or authServer.
 * @param {Object<string,any>} [data] - the initial associated data.
 */
ServerConnection.prototype.join = async function(group, username, credentials, data) {
    let m = {
        type: 'join',
        kind: 'join',
        group: group,
    };
    if(typeof username !== 'undefined' && username !== null)
        m.username = username;

    if((typeof credentials) === 'string') {
        m.password = credentials;
    } else {
        switch(credentials.type) {
        case 'password':
            m.password = credentials.password;
            break;
        case 'token':
            m.token = credentials.token;
            break;
        case 'authServer':
            let r = await fetch(credentials.authServer, {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify({
                    location: credentials.location,
                    username: username,
                    password: credentials.password,
                }),
            });
            if(!r.ok)
                throw new Error(
                    `The authorisation server said ${r.status} ${r.statusText}`,
                );
            if(r.status === 204) {
                // no data, fallback to password auth
                m.password = credentials.password;
                break;
            }
            let ctype = r.headers.get("Content-Type");
            if(!ctype)
                throw new Error(
                    "The authorisation server didn't return a content type",
                );
            let semi = ctype.indexOf(";");
            if(semi >= 0)
                ctype = ctype.slice(0, semi);
            ctype = ctype.trim();
            switch(ctype.toLowerCase()) {
            case 'application/jwt':
                let data = await r.text();
                if(!data)
                    throw new Error(
                        "The authorisation server returned empty token",
                    );
                m.token = data;
                break;
            default:
                throw new Error(`The authorisation server returned ${ctype}`);
                break;
            }
            break;
        default:
            throw new Error(`Unknown credentials type ${credentials.type}`);
        }
    }

    if(data)
        m.data = data;

    this.send(m);
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
 * request sets the list of requested tracks
 *
 * @param {Object<string,Array<string>>} what
 *     - A dictionary that maps labels to a sequence of 'audio', 'video'
 *       or 'video-low.  An entry with an empty label '' provides the default.
 */
ServerConnection.prototype.request = function(what) {
    this.send({
        type: 'request',
        request: what,
    });
};

/**
 * findByLocalId finds an active connection with the given localId.
 * It returns null if none was find.
 *
 * @param {string} localId
 * @returns {Stream}
 */
ServerConnection.prototype.findByLocalId = function(localId) {
    if(!localId)
        return null;

    let sc = this;

    for(let id in sc.up) {
        let s = sc.up[id];
        if(s.localId === localId)
            return s;
    }
    return null;
}

/**
 * getRTCConfiguration returns the RTCConfiguration that should be used
 * with this peer connection.  This usually comes from the server, but may
 * be overridden by the onpeerconnection callback.
 *
 * @returns {RTCConfiguration}
 */
ServerConnection.prototype.getRTCConfiguration = function() {
    if(this.onpeerconnection) {
        let conf = this.onpeerconnection.call(this);
        if(conf !== null)
            return conf;
    }
    return this.rtcConfiguration;
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


    let pc = new RTCPeerConnection(sc.getRTCConfiguration());
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
 * @param {any} [value] - An action-dependent parameter.
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
 * @param {any} [data]
 */
ServerConnection.prototype.groupAction = function(kind, data) {
    this.send({
        type: 'groupaction',
        source: this.id,
        kind: kind,
        username: this.username,
        value: data,
    });
};

/**
 * gotOffer is called when we receive an offer from the server.  Don't call this.
 *
 * @param {string} id
 * @param {string} label
 * @param {string} source
 * @param {string} username
 * @param {string} sdp
 * @param {string} replace
 * @function
 */
ServerConnection.prototype.gotOffer = async function(id, label, source, username, sdp, replace) {
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
            pc = new RTCPeerConnection(sc.getRTCConfiguration());
        } catch(e) {
            console.error(e);
            sc.send({
                type: 'abort',
                id: id,
            });
            return;
        }
        c = new Stream(this, id, oldLocalId || newLocalId(), pc, false);
        c.label = label;
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
            if(e.streams.length < 1) {
                console.error("Got track with no stream");
                return;
            }
            c.stream = e.streams[0];
            let changed = recomputeUserStreams(sc, source);
            if(c.ondowntrack) {
                c.ondowntrack.call(
                    c, e.track, e.transceiver, e.streams[0],
                );
            }
            if(changed && sc.onuser)
                sc.onuser.call(sc, source, "change");
        };
    }

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
 * gotAnswer is called when we receive an answer from the server.  Don't
 * call this.
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
 * gotRenegotiate is called when we receive a renegotiation request from
 * the server.  Don't call this.
 *
 * @param {string} id
 * @function
 */
ServerConnection.prototype.gotRenegotiate = function(id) {
    let c = this.up[id];
    if(!c)
        throw new Error('unknown up stream');
    c.restartIce();
};

/**
 * gotClose is called when we receive a close request from the server.
 * Don't call this.
 *
 * @param {string} id
 */
ServerConnection.prototype.gotClose = function(id) {
    let c = this.down[id];
    if(!c) {
        console.warn('unknown down stream', id);
        return;
    }
    c.close();
};

/**
 * gotAbort is called when we receive an abort message from the server.
 * Don't call this.
 *
 * @param {string} id
 */
ServerConnection.prototype.gotAbort = function(id) {
    let c = this.up[id];
    if(!c)
        throw new Error('unknown up stream');
    c.close();
};

/**
 * gotRemoteIce is called when we receive an ICE candidate from the server.
 * Don't call this.
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
     * The label assigned by the originator to this stream.
     *
     * @type {string}
     */
    this.label = null;
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
     * onerror is called whenever a fatal error occurs.  The stream will
     * then be closed, and onclose called normally.
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
     * @type{(this: Stream, track: MediaStreamTrack, transceiver: RTCRtpTransceiver, stream: MediaStream) => void}
     */
    this.ondowntrack = null;
    /**
     * onstatus is called whenever the status of the stream changes.
     *
     * @type{(this: Stream, status: string) => void}
     */
    this.onstatus = null;
    /**
     * onstats is called when we have new statistics about the connection
     *
     * @type{(this: Stream, stats: Object<unknown,unknown>) => void}
     */
    this.onstats = null;
}

/**
 * setStream sets the stream of an upwards connection.
 *
 * @param {MediaStream} stream
 */
Stream.prototype.setStream = function(stream) {
    let c = this;
    c.stream = stream;
    let changed = recomputeUserStreams(c.sc, c.sc.id);
    if(changed && c.sc.onuser)
        c.sc.onuser.call(c.sc, c.sc.id, "change");
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

    let userid;
    if(c.up) {
        userid = c.sc.id;
        if(c.sc.up[c.id] === c)
            delete(c.sc.up[c.id]);
        else
            console.warn('Closing unknown stream');
    } else {
        userid = c.source;
        if(c.sc.down[c.id] === c)
            delete(c.sc.down[c.id]);
        else
            console.warn('Closing unknown stream');
    }
    let changed = recomputeUserStreams(c.sc, userid);
    if(changed && c.sc.onuser)
        c.sc.onuser.call(c.sc, userid, "change");

    if(c.onclose)
        c.onclose.call(c, replace);

    c.sc = null;
};

/**
 * recomputeUserStreams recomputes the user.streams array for a given user.
 * It returns true if anything changed.
 *
 * @param {ServerConnection} sc
 * @param {string} id
 * @returns {boolean}
 */
function recomputeUserStreams(sc, id) {
    let user = sc.users[id];
    if(!user) {
        console.warn("recomputing streams for unknown user");
        return false;
    }

    let streams = id === sc.id ? sc.up : sc.down;
    let old = user.streams;
    user.streams = {};
    for(id in streams) {
        let c = streams[id];
        if(!c.stream)
            continue;
        if(!user.streams[c.label])
            user.streams[c.label] = {};
        c.stream.getTracks().forEach(t => {
            user.streams[c.label][t.kind] = true;
        });
    }

    return JSON.stringify(old) != JSON.stringify(user.streams);
}

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
 * gotLocalIce is Called when we get a local ICE candidate.  Don't call this.
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
 *
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
 *
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

    c.sc.send({
        type: 'offer',
        source: c.sc.id,
        username: c.sc.username,
        kind: this.localDescriptionSent ? 'renegotiate' : '',
        id: c.id,
        replace: this.replace,
        label: c.label,
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
 * request sets the list of tracks.  If this is not called, or called with
 * a null argument, then the default is provided by ServerConnection.request.
 *
 * @param {Array<string>} what - a sequence of 'audio', 'video' or 'video-low'.
 */
Stream.prototype.request = function(what) {
    let c = this;
    c.sc.send({
        type: 'requestStream',
        id: c.id,
        request: what,
    });
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
                    let id = stid;
                    // Firefox doesn't implement rid, use ssrc
                    // to discriminate simulcast tracks.
                    id = id + '-' + r.ssrc;
                    if(!('bytesSent' in r))
                        continue;
                    if(!stats[id])
                        stats[id] = {};
                    stats[id][r.type] = {};
                    stats[id][r.type].timestamp = r.timestamp;
                    stats[id][r.type].bytesSent = r.bytesSent;
                    if(old[id] && old[id][r.type])
                        stats[id][r.type].rate =
                        ((r.bytesSent - old[id][r.type].bytesSent) * 1000 /
                         (r.timestamp - old[id][r.type].timestamp)) * 8;
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
                if(rtid && r.type === 'inbound-rtp') {
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


/**
 * A file in the process of being transferred.
 * These are stored in the ServerConnection.transferredFiles dictionary.
 *
 * State transitions:
 * @example
 * '' -> inviting -> connecting -> connected -> done -> closed
 * any -> cancelled -> closed
 *
 *
 * @parm {ServerConnection} sc
 * @parm {string} userid
 * @parm {string} rid
 * @parm {boolean} up
 * @parm {string} username
 * @parm {string} mimetype
 * @parm {number} size
 * @constructor
 */
function TransferredFile(sc, userid, id, up, username, name, mimetype, size) {
    /**
     * The server connection this file is associated with.
     *
     * @type {ServerConnection}
     */
    this.sc = sc;
    /** The id of the remote peer.
     *
     * @type {string}
     */
    this.userid = userid;
    /**
     * The id of this file transfer.
     *
     * @type {string}
     */
    this.id = id;
    /**
     * True if this is an upload.
     *
     * @type {boolean}
     */
    this.up = up;
    /**
     * The state of this file transfer.  See the description of the
     * constructor for possible state transitions.
     *
     * @type {string}
     */
    this.state = '';
    /**
     * The username of the remote peer.
     *
     * @type {string}
     */
    this.username = username;
    /**
     * The name of the file being transferred.
     *
     * @type {string}
     */
    this.name = name;
    /**
     * The MIME type of the file being transferred.
     *
     * @type {string}
     */
    this.mimetype = mimetype;
    /**
     * The size in bytes of the file being transferred.
     *
     * @type {number}
     */
    this.size = size;
    /**
     * The file being uploaded.  Unused for downloads.
     *
     * @type {File}
     */
    this.file = null;
    /**
     * The peer connection used for the transfer.
     *
     * @type {RTCPeerConnection}
     */
    this.pc = null;
    /**
     * The datachannel used for the transfer.
     *
     * @type {RTCDataChannel}
     */
    this.dc = null;
    /**
     * Buffered remote ICE candidates.
     *
     * @type {Array<RTCIceCandidateInit>}
     */
    this.candidates = [];
    /**
     * The data received to date, stored as a list of blobs or array buffers,
     * depending on what the browser supports.
     *
     * @type {Array<Blob|ArrayBuffer>}
     */
    this.data = [];
    /**
     * The total size of the data received to date.
     *
     * @type {number}
     */
    this.datalen = 0;
    /**
     * The main filetransfer callback.
     *
     * This is called whenever the state of the transfer changes,
     * but may also be called multiple times in a single state, for example
     * in order to display a progress bar.  Call this.cancel in order
     * to cancel the transfer.
     *
     * @type {(this: TransferredFile, type: string, [data]: string) => void}
     */
    this.onevent = null;
}

/**
 * The full id of this file transfer, used as a key in the transferredFiles
 * dictionary.
 */
TransferredFile.prototype.fullid = function() {
    return this.userid + (this.up ? '+' : '-') + this.id;
};

/**
 * Retrieve a transferred file from the transferredFiles dictionary.
 *
 * @param {string} userid
 * @param {string} fileid
 * @param {boolean} up
 * @returns {TransferredFile}
 */
ServerConnection.prototype.getTransferredFile = function(userid, fileid, up) {
    return this.transferredFiles[userid + (up ? '+' : '-') + fileid];
};

/**
 * Close a file transfer and remove it from the transferredFiles dictionary.
 * Do not call this, call 'cancel' instead.
 */
TransferredFile.prototype.close = function() {
    let f = this;
    if(f.state === 'closed')
        return;
    if(f.state !== 'done' && f.state !== 'cancelled')
        console.warn(
            `TransferredFile.close called in unexpected state ${f.state}`,
        );
    if(f.dc) {
        f.dc.onclose = null;
        f.dc.onerror = null;
        f.dc.onmessage = null;
    }
    if(f.pc)
        f.pc.close();
    f.dc = null;
    f.pc = null;
    f.data = [];
    f.datalen = 0;
    delete(f.sc.transferredFiles[f.fullid()]);
    f.event('closed');
}

/**
 * Buffer a chunk of data received during a file transfer.
 * Do not call this, it is called automatically when data is received.
 *
 * @param {Blob|ArrayBuffer} data
 */
TransferredFile.prototype.bufferData = function(data) {
    let f = this;
    if(f.up)
        throw new Error('buffering data in the wrong direction');
    if(data instanceof Blob) {
        f.datalen += data.size;
    } else if(data instanceof ArrayBuffer) {
        f.datalen += data.byteLength;
    } else {
        throw new Error('unexpected type for received data');
    }
    f.data.push(data);
}

/**
 * Retreive the data buffered during a file transfer.  Don't call this.
 *
 * @returns {Blob}
 */
TransferredFile.prototype.getBufferedData = function() {
    let f = this;
    if(f.up)
        throw new Error('buffering data in wrong direction');
    let blob = new Blob(f.data, {type: f.mimetype});
    if(blob.size != f.datalen)
        throw new Error('Inconsistent data size');
    f.data = [];
    f.datalen = 0;
    return blob;
}

/**
 * Set the file's state, and call the onevent callback.
 *
 * This calls the callback even if the state didn't change, which is
 * useful if the client needs to display a progress bar.
 *
 * @param {string} state
 * @param {any} [data]
 */
TransferredFile.prototype.event = function(state, data) {
    let f = this;
    f.state = state;
    if(f.onevent)
        f.onevent.call(f, state, data);
}


/**
 * Cancel a file transfer.
 *
 * Depending on the state, this will either forcibly close the connection,
 * send a handshake, or do nothing.  It will set the state to cancelled.
 *
 * @param {string|Error} [data]
 */
TransferredFile.prototype.cancel = function(data) {
    let f = this;
    if(f.state === 'closed')
        return;
    if(f.state !== '' && f.state !== 'done' && f.state !== 'cancelled') {
        let m = {
            type: f.up ? 'cancel' : 'reject',
            id: f.id,
        };
        if(data)
            m.message = data.toString();
        f.sc.userMessage('filetransfer', f.userid, m);
    }
    if(f.state !== 'done' && f.state !== 'cancelled')
        f.event('cancelled', data);
    f.close();
}

/**
 * Forcibly terminate a file transfer.
 *
 * This is like cancel, but will not attempt to handshake.
 * Use cancel instead of this, unless you know what you are doing.
 *
 * @param {string|Error} [data]
 */
TransferredFile.prototype.fail = function(data) {
    let f = this;
    if(f.state === 'done' || f.state === 'cancelled' || f.state === 'closed')
        return;
    f.event('cancelled', data);
    f.close();
}

/**
 * Initiate a file upload.
 *
 * This will cause the onfiletransfer callback to be called, at which
 * point you should set up the onevent callback.
 *
 * @param {string} id
 * @param {File} file
 */
ServerConnection.prototype.sendFile = function(id, file) {
    let sc = this;
    let fileid = newRandomId();
    let user = sc.users[id];
    if(!user)
        throw new Error('offering upload to unknown user');
    let f = new TransferredFile(
        sc, id, fileid, true, user.username, file.name, file.type, file.size,
    );
    f.file = file;

    try {
        if(sc.onfiletransfer)
            sc.onfiletransfer.call(sc, f);
        else
            throw new Error('this client does not implement file transfer');
    } catch(e) {
        f.cancel(e);
        return;
    }

    sc.transferredFiles[f.fullid()] = f;
    sc.userMessage('filetransfer', id, {
        type: 'invite',
        id: fileid,
        name: f.name,
        size: f.size,
        mimetype: f.mimetype,
    });
    f.event('inviting');
}

/**
 * Receive a file.
 *
 * Call this after the onfiletransfer callback has yielded an incoming
 * file (up field set to false).  If you wish to reject the file transfer,
 * call cancel instead.
 */
TransferredFile.prototype.receive = async function() {
    let f = this;
    if(f.up)
        throw new Error('Receiving in wrong direction');
    if(f.pc)
        throw new Error('Download already in progress');
    let pc = new RTCPeerConnection(f.sc.getRTCConfiguration());
    if(!pc) {
        let err = new Error("Couldn't create peer connection");
        f.fail(err);
        return;
    }
    f.pc = pc;
    f.event('connecting');

    f.candidates = [];
    pc.onsignalingstatechange = function(e) {
        if(pc.signalingState === 'stable') {
            f.candidates.forEach(c => pc.addIceCandidate(c).catch(console.warn));
            f.candidates = [];
        }
    };
    pc.onicecandidate = function(e) {
        f.sc.userMessage('filetransfer', f.userid, {
            type: 'downice',
            id: f.id,
            candidate: e.candidate,
        });
    };
    f.dc = pc.createDataChannel('file');
    f.data = [];
    f.datalen = 0;
    f.dc.onclose = function(e) {
        f.cancel('remote peer closed connection');
    };
    f.dc.onmessage = function(e) {
        f.receiveData(e.data).catch(e => f.cancel(e));
    };
    f.dc.onerror = function(e) {
        /** @ts-ignore */
        let err = e.error;
        f.cancel(err)
    };
    let offer = await pc.createOffer();
    if(!offer) {
        f.cancel(new Error("Couldn't create offer"));
        return;
    }
    await pc.setLocalDescription(offer);
    f.sc.userMessage('filetransfer', f.userid, {
        type: 'offer',
        id: f.id,
        sdp: pc.localDescription.sdp,
    });
}

/**
 * Negotiate a file transfer on the sender side.
 * Don't call this, it is called automatically we receive an offer.
 *
 * @param {string} sdp
 */
TransferredFile.prototype.answer = async function(sdp) {
    let f = this;
    if(!f.up)
        throw new Error('Sending file in wrong direction');
    if(f.pc)
        throw new Error('Transfer already in progress');
    let pc = new RTCPeerConnection(f.sc.getRTCConfiguration());
    if(!pc) {
        let err = new Error("Couldn't create peer connection");
        f.fail(err);
        return;
    }
    f.pc = pc;
    f.event('connecting');

    f.candidates = [];
    pc.onicecandidate = function(e) {
        f.sc.userMessage('filetransfer', f.userid, {
            type: 'upice',
            id: f.id,
            candidate: e.candidate,
        });
    };
    pc.onsignalingstatechange = function(e) {
        if(pc.signalingState === 'stable') {
            f.candidates.forEach(c => pc.addIceCandidate(c).catch(console.warn));
            f.candidates = [];
        }
    };
    pc.ondatachannel = function(e) {
        if(f.dc) {
            f.cancel(new Error('Duplicate datachannel'));
            return;
        }
        f.dc = /** @type{RTCDataChannel} */(e.channel);
        f.dc.onclose = function(e) {
            f.cancel('remote peer closed connection');
        };
        f.dc.onerror = function(e) {
            /** @ts-ignore */
            let err = e.error;
            f.cancel(err);
        }
        f.dc.onmessage = function(e) {
            if(e.data === 'done' && f.datalen === f.size) {
                f.event('done');
                f.dc.onclose = null;
                f.dc.onerror = null;
                f.close();
            } else {
                f.cancel(new Error('unexpected data from receiver'));
            }
        }
        f.send().catch(e => f.cancel(e));
    };

    await pc.setRemoteDescription({
        type: 'offer',
        sdp: sdp,
    });

    let answer = await pc.createAnswer();
    if(!answer)
        throw new Error("Couldn't create answer");
    await pc.setLocalDescription(answer);
    f.sc.userMessage('filetransfer', f.userid, {
        type: 'answer',
        id: f.id,
        sdp: pc.localDescription.sdp,
    });

    f.event('connected');
}

/**
 * Transfer file data.  Don't call this, it is called automatically
 * after negotiation completes.
 */
TransferredFile.prototype.send = async function() {
    let f = this;
    if(!f.up)
        throw new Error('sending in wrong direction');
    let r = f.file.stream().getReader();

    f.dc.bufferedAmountLowThreshold = 65536;

    /** @param {Uint8Array} a */
    async function write(a) {
        while(f.dc.bufferedAmount > f.dc.bufferedAmountLowThreshold) {
            await new Promise((resolve, reject) => {
                if(!f.dc) {
                    reject(new Error('File is closed.'));
                    return;
                }
                f.dc.onbufferedamountlow = function(e) {
                    if(!f.dc) {
                        reject(new Error('File is closed.'));
                        return;
                    }
                    f.dc.onbufferedamountlow = null;
                    resolve();
                }
            });
        }
        f.dc.send(a);
        f.datalen += a.length;
        // we're already in the connected state, but invoke callbacks to
        // that the application can display progress
        f.event('connected');
    }

    while(true) {
        let v = await r.read();
        if(v.done)
            break;
        let data = v.value;
        if(!(data instanceof Uint8Array))
            throw new Error('Unexpected type for chunk');
        /* Base SCTP only supports up to 16kB data chunks.  There are
           extensions to handle larger chunks, but they don't interoperate
           between browsers, so we chop the file into small pieces. */
        if(data.length <= 16384) {
            await write(data);
        } else {
            for(let i = 0; i < v.value.length; i += 16384) {
                let d = data.subarray(i, Math.min(i + 16384, data.length));
                await write(d);
            }
        }
    }
}

/**
 * Called after we receive an answer.  Don't call this.
 *
 * @param {string} sdp
 */
TransferredFile.prototype.receiveFile = async function(sdp) {
    let f = this;
    if(f.up)
        throw new Error('Receiving in wrong direction');
    await f.pc.setRemoteDescription({
        type: 'answer',
        sdp: sdp,
    });
    f.event('connected');
}

/**
 * Called whenever we receive a chunk of data.  Don't call this.
 *
 * @param {Blob|ArrayBuffer} data
 */
TransferredFile.prototype.receiveData = async function(data) {
    let f = this;
    if(f.up)
        throw new Error('Receiving in wrong direction');
    f.bufferData(data);

    if(f.datalen < f.size) {
        f.event('connected');
        return;
    }

    f.dc.onmessage = null;

    if(f.datalen != f.size) {
        f.cancel('extra data at end of file');
        return;
    }

    let blob = f.getBufferedData();
    if(blob.size != f.size) {
        f.cancel("inconsistent data size (this shouldn't happen)");
        return;
    }
    f.event('done', blob);

    // we've received the whole file.  Send the final handshake, but don't
    // complain if the peer has closed the channel in the meantime.
    await new Promise((resolve, reject) => {
        let timer = setTimeout(function(e) { resolve(); }, 2000);
        f.dc.onclose = function(e) {
            clearTimeout(timer);
            resolve();
        };
        f.dc.onerror = function(e) {
            clearTimeout(timer);
            resolve();
        };
        f.dc.send('done');
    });

    f.close();
}

/**
 * fileTransfer handles a usermessage of kind 'filetransfer'.  Don't call
 * this, it is called automatically as needed.
 *
 * @param {string} id
 * @param {string} username
 * @param {object} message
 */
ServerConnection.prototype.fileTransfer = function(id, username, message) {
    let sc = this;
    switch(message.type) {
    case 'invite': {
        let f = new TransferredFile(
            sc, id, message.id, false, username,
            message.name, message.mimetype, message.size,
        );
        f.state = 'inviting';

        try {
            if(sc.onfiletransfer)
                sc.onfiletransfer.call(sc, f);
            else {
                f.cancel('this client does not implement file transfer');
                return;
            }
        } catch(e) {
            f.cancel(e);
            return;
        }

        if(f.fullid() in sc.transferredFiles) {
            console.error('Duplicate id for file transfer');
            f.cancel("duplicate id (this shouldn't happen)");
            return;
        }
        sc.transferredFiles[f.fullid()] = f;
        break;
    }
    case 'offer': {
        let f = sc.getTransferredFile(id, message.id, true);
        if(!f) {
            console.error('Unexpected offer for file transfer');
            return;
        }
        f.answer(message.sdp).catch(e => f.cancel(e));
        break;
    }
    case 'answer': {
        let f = sc.getTransferredFile(id, message.id, false);
        if(!f) {
            console.error('Unexpected answer for file transfer');
            return;
        }
        f.receiveFile(message.sdp).catch(e => f.cancel(e));
        break;
    }
    case 'downice':
    case 'upice': {
        let f = sc.getTransferredFile(
            id, message.id, message.type === 'downice',
        );
        if(!f || !f.pc) {
            console.warn(`Unexpected ${message.type} for file transfer`);
            return;
        }
        if(f.pc.signalingState === 'stable')
            f.pc.addIceCandidate(message.candidate).catch(console.warn);
        else
            f.candidates.push(message.candidate);
        break;
    }
    case 'cancel':
    case 'reject': {
        let f = sc.getTransferredFile(id, message.id, message.type === 'reject');
        if(!f) {
            console.error(`Unexpected ${message.type} for file transfer`);
            return;
        }
        f.event('cancelled', message.value || null);
        f.close();
        break;
    }
    default:
        console.error(`Unknown filetransfer message ${message.type}`);
        break;
    }
}
