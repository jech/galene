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

Before it connects and joins a group, a client may perform an HTTP GET
request on the URL `/public-groups.json`.  This yields a JSON array of
objects, one for each group that has been marked public in its
configuration file.  Each object has the following fields:

 - `name`: the group's name
 - `displayName` (optional): a longer version of the name used for display;
 - `description` (optional): a user-readable description.
 - `locked`: true if the group is locked;
 - `clientCount`: the number of clients currently in the group.

A client may also fetch the URL `/group/name/.status.json` to retrieve the
status of a single group.  If the group has not been marked as public,
then the fields `locked` and `clientCount` are omitted.

## Connecting

The client connects to the websocket at `/ws`.  Galene uses a symmetric,
asynchronous protocol: there are no requests and responses, and most
messages may be sent by either peer.

## Message syntax

All messages are sent as JSON objects.  All fields except `type` are
optional; however, there are some fields that are common across multiple
message types:

 - `type`, the type of the message;
 - `kind`, the subtype of the message;
 - `id`, the id of the object being manipulated;
 - `source`, the client-id of the originating client;
 - `username`, the username of the originating client;
 - `dest`, the client-id of the destination client;
 - `privileged`, set by the server to indicate that the originating client
   had the `op` privilege at the time when it sent the message.

## Establishing and maintaining a connection

The peer establishing the connection (the WebSocket client) sends
a handshake message.  The server replies with another handshake message.
The client may wait for the server's handshake, or it may immediately
start pipelining messages to the server.

```javascript
{
    type: 'handshake',
    id: id
}
```

If the field `id` is absent, then the peer doesn't originate streams (it
is a server).

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
    password: password
}
```

When the sender has effectively joined the group, the peer will send
a 'joined' message of kind 'join'; it may then send a 'joined' message of
kind 'change' at any time, in order to inform the client of a change in
its permissions or in the recommended RTC configuration.

```javascript
{
    type: 'joined',
    kind: 'join' or 'fail' or 'change' or 'leave',
    group: group,
    username: username,
    permissions: permissions,
    status: status,
    rtcConfiguration: RTCConfiguration
}
```

The `permissions` field is an array of strings that may contain the values
`present`, `op` and `record`.  The `status` field is a dictionary that
contains status information about the group, in the same format as at the
`.status.json` URL above.

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
or use rid-based simulcasting.  In the latter case, it should send two
video streams, one with rid 'h' and high throughput, and one with rid 'l'
and throughput limited to roughly 100kbit/s.

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
field `noecho` is set by the client if it doesn't wish to receive a copy
of its own message.

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
`kick` and `setstatus`.

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
`clearchat` user message), `lock`, `unlock`, `record`, `unrecord` and
`subgroups`.
