# Galène's protocol

## Data structures

### Group

A group is a set of clients.  It is identified by a human-readable name
that must not start or end with a slash "`/`", must not start with
a period "`.`", and must not contain the substrings "`/../`" or "`/./`".

### Client

A client is a peer that may originate offers and chat messages.  It is
identified by an id, an opaque string that is assumed to be unique.  Peers
that do not originate messages (servers) do not need to be assigned an id.

### Stream

A stream is a set of related tracks.  It is identified by an id, an opaque
string.  Streams in Galène are unidirectional.  A stream is carried by
exactly one peer connection (PC) (multiple streams in a single PC are not
allowed).  The offerer is also the RTP sender (i.e. all tracks sent by the
offerer are of type `sendonly`).

Galène uses a symmetric, asynchronous protocol.  In client-server
usage, some messages are only sent in the client to server or in the
server to client direction.

## Before connecting

The client needs to know the location of the group, the (user-visible) URL
at which the group is found.  This may be obtained either by explicit
configuration by the user, or by parsing the `/public-groups.json` file
which may contain an array of group statuses (see below).

A client then performs an HTTP GET request on the file `.status` at
the group's location.  This yields a single JSON object, which contains
the following fields:

 - `name`: the group's name
 - `location`: the group's location
 - `endpoint`: the URL of the server's WebSocket endpoint
 - `displayName`: a longer version of the name used for display;
 - `description`: a user-readable description;
 - `authServer`: the URL of the authentication server, if any;
 - `authPortal`: the uRL of the authentication portal, if any;
 - `locked`: true if the group is locked;
 - `clientCount`: the number of clients currently in the group.

All fields are optional except `name`, `location` and `endpoint`.


## Connecting

The client connects to the websocket at the URL obtained at the previous
step.  Galene uses a symmetric, asynchronous protocol: there are no
requests and responses, and most messages may be sent by either peer.

## Message syntax

All messages are sent as JSON objects.  All fields except `type` are
optional; however, there are some fields that are common across multiple
message types:

 - `type`, the type of the message;
 - `kind`, the subtype of the message;
 - `error`, indicates that the message is an error indication, and
   specifies the kind of error that occurred;
 - `id`, the id of the object being manipulated;
 - `source`, the client-id of the originating client;
 - `username`, the username of the originating client;
 - `dest`, the client-id of the destination client;
 - `privileged`, set by the server to indicate that the originating client
   had the `op` privilege at the time when it sent the message.
 - `value`, the value of the message (which can be of any type).

There are two kinds of errors.  Unsolicited errors are sent using messages
of type `usermessage` of kind `error` or `warning`.  Errors sent in reply
to a message use the same type as the usual reply, but with a specific
kind (such as `fail`).  In either case, the field `value` contains
a human-readable error message, while the field `error`, if present,
contains a stable, program-readable identifier for the error.

## Establishing and maintaining a connection

The peer establishing the connection (the WebSocket client) sends
a handshake message.  The server replies with another handshake message.
The client may wait for the server's handshake, or it may immediately
start pipelining messages to the server.

```javascript
{
    type: 'handshake',
    version: ["2"],
    id: id
}
```

The version field contains an array of supported protocol versions, in
decreasing preference order; the client may announce multiple versions,
but the server will always reply with a single version.  If the field `id`
is absent, then the peer doesn't originate streams.

A peer may, at any time, send a `ping` message.

```javascript
{
    type: 'ping'
}
```

The receiving peer must reply with a `pong` message within 30s.

```javascript
{
    type: 'pong'
}
```

## Joining and leaving

The `join` message requests that the sender join or leave a group:

```javascript
{
    type: 'join',
    kind: 'join' or 'leave',
    group: group,
    username: username,
    password: password,
    data: data
}
```

If token-based authorisation is beling used, then the `username` and
`password` fields are omitted, and a `token` field is included instead.

When the sender has effectively joined the group, the peer will send
a 'joined' message of kind 'join'; it may then send a 'joined' message of
kind 'change' at any time, in order to inform the client of a change in
its permissions or in the recommended RTC configuration.

```javascript
{
    type: 'joined',
    kind: 'join' or 'fail' or 'change' or 'leave',
    error: may be set if kind is 'fail',
    group: group,
    username: username,
    permissions: permissions,
    status: status,
    data: data,
    rtcConfiguration: RTCConfiguration
}
```

The `username` field is the username that the server assigned to this
user.  The `permissions` field is an array of strings that may contain the
values `present`, `op` and `record`.  The `status` field is a dictionary
that contains status information about the group, and updates the data
obtained from the `.status` URL described above.

## Maintaining group membership

Whenever a user joins or leaves a group, the server will send all other
users a `user` message:

```javascript
{
    type: 'user',
    kind: 'add' or 'change' or 'delete',
    id: id,
    username: username,
    permissions: permissions,
    status: status
}
```

## Requesting streams

A peer must explicitly request the streams that it wants to receive.

```javascript
{
    type: 'request',
    request: requested
}
```

The field `request` is a dictionary that maps the labels of requested
streams to a list containing either 'audio', or one of 'video' or
'video-low'.  The empty key `''` serves as default.  For example:

```javascript
{
    type: 'request',
    request: {
        camera: ['audio', 'video-low'],
        '': ['audio', 'video']
    }
}
```

## Pushing streams

A stream is created by the sender with the `offer` message:

```javascript
{
    type: 'offer',
    id: id,
    label: label,
    replace: id,
    source: source-id,
    username: username,
    sdp: sdp
}
```

If a stream with the same id exists, then this is a renegotiation;
otherwise this message creates a new stream.  If the field `replace` is
not empty, then this request additionally requests that an existing stream
with the given id should be closed, and the new stream should replace it;
this is used most notably when changing the simulcast envelope.

The field `label` is one of `camera`, `screenshare` or `video`, and will
be matched against the keys sent by the receiver in their `request` message.

The field `sdp` contains the raw SDP string (i.e. the `sdp` field of
a JSEP session description).  Galène will interpret the `nack`,
`nack pli`, `ccm fir` and `goog-remb` RTCP feedback types, and act
accordingly.

The sender may either send a single stream per media section in the SDP,
or use rid-based simulcasting with the streams ordered in decreasing order
of throughput.  In that case, it should send two video streams, the
first one with high throughput, and the second one with throughput limited
to roughly 100kbit/s.  If more than two streams are sent, then only the
first and the last one will be considered.

The receiver may either abort the stream immediately (see below), or send
an answer.

```javascript
{
    type: 'answer',
    id: id,
    sdp: SDP
}
```

Both peers may then trickle ICE candidates with `ice` messages.

```javascript
{
    type: 'ice',
    id: id,
    candidate: candidate
}
```

The answerer may request a new offer of kind `renegotiate` and an ICE
restart by sending a `renegotiate` message:

```javascript
{
    type: 'renegotiate',
    id: id
}
```

At any time after answering, the client may change the set of streams
being offered by sending a 'requestStream' request:
```javascript
{
    type: 'requestStream'
    id: id,
    request: [audio, video]
}
```

## Closing streams

The offerer may close a stream at any time by sending a `close` message.

```javascript
{
    type: 'close',
    id: id
}
```

The answerer may request that the offerer close a stream by sending an
`abort` message.

```javascript
{
    type: 'abort',
    id: id
}
```

The stream will not be effectively closed until the offerer sends
a matching `close`.

## Sending messages

A chat message may be sent using a `chat` message.

```javascript
{
    type: 'chat',
    kind: '' or 'me',
    source: source-id,
    username: username,
    dest: dest-id,
    privileged: boolean,
    time: time,
    noecho: false,
    value: message
}
```

If `dest` is empty, the message is a broadcast message, destined to all of
the clients in the group.  If `source` is empty, then the message was
originated by the server.  The message is forwarded by the server without
interpretation, the server only validates that the `source` and `username`
fields are authentic.  The field `privileged` is set to true by the server
if the message was originated by a client with the `op` permission.  The
field `time` is the timestamp of the message, coded as a number in version
1 of the protocol, and as a string in ISO 8601 format in later versions.
The field `noecho` is set by the client if it doesn't wish to receive
a copy of its own message.

The `chathistory` message is similar to the `chat` message, but carries
a message taken from the chat history.  Most clients should treat
`chathistory` similarly to `chat`.

A user message is similar to a chat message, but is not conserved in the
chat history, and is not expected to contain user-visible content.

```javascript
{
    type: 'usermessage',
    kind: kind,
    source: source-id,
    username: username,
    dest: dest-id,
    privileged: boolean,
    value: value
}
```

Currently defined kinds include `error`, `warning`, `info`, `kicked`,
`clearchat` (not to be confused with the `clearchat` group action), and
`mute`.

A user action requests that the server act upon a user.

```javascript
{
    type: 'useraction',
    kind: kind,
    source: source-id,
    username: username,
    dest: dest-id,
    value: value
}
```
Currently defined kinds include `op`, `unop`, `present`, `unpresent`,
`kick` and `setdata`.

Finally, a group action requests that the server act on the current group.

```javascript
{
    type: 'groupaction',
    kind: kind,
    source: source-id,
    username: username,
    value: value
}
```

Currently defined kinds include `clearchat` (not to be confused with the
`clearchat` user message), `lock`, `unlock`, `record`, `unrecord`,
`subgroups` and `setdata`.

# Authorisation protocol

In addition to username/password authentication, Galene supports
authentication using cryptographic tokens.  Two flows are supported: using
an authentication server, where Galene's client requests a token from
a third-party server, and using an authentication portal, where
a third-party login portal redirects the user to Galene.  Authentication
servers are somewhat simpler to implement, but authentication portals are
more flexible and avoid communicating the user's password to Galene's
Javascript code.

## Authentication server

If a group's status dictionary has a non-empty `authServer` field, then
the group uses an authentication server.  Before joining, the client sends
a POST request to the authorisation server URL containing in its body
a JSON dictionary of the following form:
```javascript
{
    "location": "https://galene.example.org/group/groupname/",
    "username": username,
    "password": password
}
```

If the user is not allowed to join the group, then the authorisation
server replies with a code of 403 ("not authorised"), and Galene will
reject the user.  If the authentication server has no opinion about
whether the user is allowed to join, it replies with a code of 204 ("no
content"), and Galene will proceed with ordinary password authorisation.

If the user is allowed to join, then the authorisation server replies with
a signed JWT (a "JWS") the body of which has the following form:
```javascript
{
    "sub": username,
    "aud": "https://galene.example.org/group/groupname/",
    "permissions": ["present"],
    "iat": now,
    "exp": now + 30s,
    "iss": authorisation server URL
}
```
The `permissions` field contains the permissions granted to the client, in
the same format as in the `joined` message.  Since the client will only
use the token once, at the very beginning of the session, the tokens
issued may have a short lifetime (on the order of 30s).

## Authentication portal

If a group's status dictionary has a non-empty `authPortal` field, Galene
redirects the user agent to the URL indicated by `authPortal`.  The
authentication portal performs authorisation, generates a token as above,
then redirects back to the group's URL with the token stores in a URL
query parameter named `token`:

    https://galene.example.org/group/groupname/?token=eyJhbG...
