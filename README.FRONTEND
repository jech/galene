# Writing a new frontend

The frontend is written in JavaScript and is split into two files:

  - `protocol.js` contains the low-level functions that interact with the
    server;
  - `galene.js` contains the user interface.

A simpler example client can be found in the directory `static/example`.

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

First, fetch the `.status` JSON at the group URL:

```javascript
let r = await fetch(url + ".status");
if(!r.ok) {
    throw new Error(`${r.status} ${r.statusText}`);
}
let status = await r.json();
```

Create a `ServerConnection` and set up all the callbacks:

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

You may now connect to the server:

```javascript
serverConnection.connect(status.endpoint);
```

You typically join a group in the `onconnected` callback:

```javascript
serverConnection.onconnected = function() {
    this.join(group, 'join', username, password); 
}
```

After the server has replied to the join request, the `onjoined` callback
will trigger.  There, you update your user interface and request incoming
streams:

```javascript
serverConnection.onjoined = function(kind, group, perms, status, data, error, message) {
    switch(kind) {
    case 'join':
        this.request({'':['audio','video']});
        // then update the UI, possibly taking perms.present into account
        break;
    case 'change':
        // update the UI
        break;
    case 'redirect':
        this.close();
        document.location.href = message;
        break;
    case 'fail':
         if(error === 'need-username') {
            // the user attempted to login with a token that does not
            // specify a username.  Display a dialog requesting a username,
            // then join again
        } else {
            // display the friendly error message
        }
        break;
}
```


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

The `stream.label` field is one of `camera`, `screenshare` or `video`.

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
stream.label = ...;
stream.onerror = ...;
stream.onstatus = ...;
localStream.getTracks().forEach(t => {
    c.pc.addTrack(t, c.stream);
});
```

The `newUpStream` method takes an optional parameter.  If this is set to
the `localId` property of an existing stream, then the existing stream
will be closed and the server will be informed that the new stream
replaces the existing stream.

## Stream statistics

Some statistics about streams are made available by calling the
`setStatsInterval` method and setting the `onstats` callback.  These
include the data rate for streams in the up direction, and the average
audio energy (the square of the volume) for streams in the down direction.


# Peer-to-peer file transfer

Galene's client allows users to transfer files during a meeting.  The
protocol is peer-to-peer: the clients exchange network parameters and
cryptographic keys through the server (over messages of type
`usermessage`), but all file transfer is performed directly between the
peers.

An in-progress file transfer is represented by a JavaScript object of
class `TransferredFile`.  This object implements a finite state automaton
whose current state is encoded as a string in the field `state`.  It obeys
the following state transitions:

```
(empty string) ⟶ inviting ⟶ connecting ⟶ connected ⟶ done ⟶ closed

(any state) ⟶ cancelled ⟶ closed
```


## Downloading files

A client that wishes to participate in the file transfer protocol must set
up the `onfiletransfer` callback of the `ServerConnection` object.

```javascript
    serverConnection.onfiletransfer = function(transfer) {
        ...
    };
```

This callback will be called whenever a file transfer is initiated, either
by the remote or by the local peer.  The callback receives a single value
of class `TransferredFile`.  It should start by setting up the `onevent`
callback, which is called whenever the state of the transfer changes and
whenever data is received:

```javascript
    transfer.onevent = func(state, data) {
        ...
    };
```

The direction of the file transfer is indicated by the value of the
boolean `this.up`, which is false in the case of a donwload.

The callback may immediately reject the file transfer by either throwing
an exception or by calling `transfer.cancel` and returning.  If the file
transfer is not immediately rejected, the callback should set up an
`onevent` callback on the `TransferredFile` object:

```javascript
    transfer.onevent = func(state, data) {
        ...
    };
```

It must then arrange for either `transfer.receive` or `transfer.cancel` to
be called, for example from an `onclick` callback.

The `onevent` callback will then be repeatedly called, which can be used
e.g. to present a progress bar to the user.  Eventually, the `onevent`
callback will be called with `state` equal to either `cancelled` or
`done`; in the latter case, the transferred data is passed as a `Blob` in
the `data` parameter of the callback.


## Uploading files

A file upload is initiated by calling the `sendFile` method of the class
`ServerConnection`.

```javascript
serverConnection.sendFile(userid, file);
```

The `userid` parameter is the id of the remote peer.  The `file` parameter
is an object of kind `File`, typically obtained from an `HTMLInputElement`
with type `file`.

The `onfiletransfer` callback is then called (with `this.up` set to true),
and the transfer proceeds analogously to a file download, except that no
data is passed to the `onevent` callback at the end of the transfer.


— Juliusz Chroboczek <https://www.irif.fr/~jch/>
