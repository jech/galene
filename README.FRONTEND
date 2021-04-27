# Writing a new frontend

The frontend is written in JavaScript and is split into two files:

  - `protocol.js` contains the low-level functions that interact with the
    server;
  - `galene.js` contains the user interface.

A new frontend may either implement Galène's client-server protocol from
scratch, or it may use the functionality of `protocol.js`.  This document
documents the latter approach.

## Data structures

The class `ServerConnection` encapsulates a connection to the server as
well as all the associated streams.  Unless your frontend communicates
with multiple servers, it will probably create just a single instance of
this class.

The class `Stream` encapsulates a set of related audio and video tracks
(for example, an audio track from a microphone and a video track from
a webcam).  A stream is said to go *up* when it carries data from the
client to the server, and *down* otherwise.  Streams going up are created
by the client (your frontend), streams going down are created by the server.

## Connecting to the server

First, create a `ServerConnection` and set up all the callbacks:

```javascript
let sc = new ServerConnection()
serverConnection.onconnected = ...;
serverConnection.onclose = ...;
serverConnection.onusermessage = ...;
serverConnection.onjoined = ...;
serverConnection.onuser = ...;
serverConnection.onchat = ...;
serverConnection.onclearchat = ...;
serverConnection.ondownstream = ...;
```

The `onconnected` callback is called when we connect to the server.  The
`onclose` callback is called when the socket is closed; all streams will
have been closed by the time it is called.  The `onusermessage` callback
indicates an application-specific message, either from another user or
from the server; the field `kind` indicates the kind of message.

Once you have joined a group (see below), the remaining callbacks may
trigger.  The `onuser` callback is used to indicate that a user has joined
or left the current group, or that their attributes have changed; the
user's state can be found in the `users` dictionary.  The `onchat`
callback indicates that a chat message has been posted to the group, and
`onclearchat` indicates that the chat history has been cleared.  Finally,
`ondownstream` is called when the server pushes a stream to the client;
see the section below about streams.

You may now connect to the server.

```javascript
serverConnection.connect(`wss://${location.host}/ws`);
```

You typically join a group and request media in the `onconnected` callback:

```javascript
serverConnection.onconnected = function() {
    this.join(group, 'join', username, password); 
    this.request('everything');
}
```

You should not attempt to push a stream to the server until it has granted
you the `present` permission through the `onjoined` callback.

## Sending and receiving chat messages

Once you have joined a group, you send chat messages with the `chat`
method of the `ServerConnection` class.  No permission is needed to do that.

```javascript
serverConnection.chat(username, '', id, 'Hi!');
```

You receive chat messages in the `onchat` callback.  The server may
request that you clear your chat window, in that case the `onclearchat`
callback will trigger.

## Other messages

The `usermessage` method of the `ServerConnection` is similar to the
`chat` method, but it sends an application-specific message.  Just like
chat messages, application-specific messages are not interpreted by the
server; unlike chat messages, they are not kept in the chat history.

The `useraction` method is used to ask the server to act on a remote user
(kick it, change its permissions, etc.); similarly, the `groupaction`
class requests an action to be performed on the current group.  Most
actions require either the `Op` or the `Record` permission.

## Accepting incoming video streams

When the server pushes a stream to the client, the `ondownstream` callback
will trigger; you should set up the stream's callbacks here.
```javascript
serverConnection.ondownstream = function(stream) {
    stream.onclose = ...;
    stream.onerror = ...;
    stream.ondowntrack = ...;
    stream.onstatus = ...;
}
```

The `stream.labels` dictionary maps each track's id to one of `audio`,
`video` or `screenshare`.  Since `stream.labels` is already available at
this point, you may set up an `audio` or `video` component straight away,
or you may choose to wait until the `ondowntrack` callback is called.

After a new stream is created, `ondowntrack` will be called whenever
a track is added.

The `onstatus` callback is invoked whenever the client library detects
a change in the status of the stream; states `connected` and `complete`
indicate a functioning stream; other states indicate that the stream is
not working right now but might recover in the future.

The `onclose` callback is called when the stream is destroyed, either by
the server or in response to a call to the `close` method.  The optional
parameter is true when the stream is being replaced by a new stream; in
that case, the call to `onclose` will be followed with a call to
`onstream` with the same `localId` value.

## Pushing outgoing video streams

If you have the `present` permission, you may use the `newUpStream` method
to push a stream to the server.  Given a `MediaStream` called `localStream`
(as obtained from `getUserMedia` or `getDisplayMedia`).

```javascript
let stream = serverConnection.newUpStream();
stream.onerror = ...;
stream.onstatus = ...;
localStream.getTracks().forEach(t => {
    c.labels[t.id] = t.kind;
    c.pc.addTrack(t, c.stream);
});
```

The `newUpStream` method takes an optional parameter.  If this is set to
the `localId` property of an existing stream, then the existing stream
will be closed and the server will be informed that the new stream
replaces the existing stream.

See above for information about setting up the `labels` dictionary.

## Stream statistics

Some statistics about streams are made available by calling the
`setStatsInterval` method and setting the `onstats` callback.  These
include the data rate for streams in the up direction, and the average
audio energy (the square of the volume) for streams in the down direction.

--- Juliusz Chroboczek <https://www.irif.fr/~jch/>
