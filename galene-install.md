# Galene installation instructions

## Basic installation

### Build the Galene binary

Say:

```sh
CGO_ENABLED=0 go build -ldflags='-s -w'
```

On Windows, say:

```dosbat
set CGO_ENABLED=0
go build -ldflags="-s -w"
```

If your server has a different architecture than the machine on which you
are building, set the `GOOS` and `GOARCH` environment variables.  For
example, in order to compile for a 64-bit ARM system (a Raspberry Pi or an
Olimex board, for example), you would say:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags='-s -w'
```

### Optional: install libraries for background blur

Galene's client uses Google's MediaPipe library to implement background
blur.  This library is optional, and if it is absent, Galene will
disable the menu entries for background blur.

Optionally install Google's MediaPipe library:

```sh
mkdir mediapipe
cd mediapipe
wget https://registry.npmjs.org/@mediapipe/tasks-vision/-/tasks-vision-0.10.21.tgz
tar xzf tasks-vision-*.tgz
rm -f ../static/third-party/tasks-vision
mv package ../static/third-party/tasks-vision
cd ../static/third-party/tasks-vision
mkdir models
cd models
wget https://storage.googleapis.com/mediapipe-models/image_segmenter/selfie_segmenter/float16/latest/selfie_segmenter.tflite
cd ../../../../
```

If you don't have `wget` on your system, try using `curl -O` instead.

### Deploy to your server

The following instructions assume that your server is called
`galene.example.org` and that you have already created a dedicated user
called `galene`.

First, make sure that the `groups` and `data` directories exist:

```sh
mkdir groups data
```

Now copy the `galene` binary, and the directories `static`, `data` and
`groups` to the server:

```sh
rsync -a galene static data groups galene@galene.example.org:
```

If you don't have a TLS certificate, Galene will generate a self-signed
certificate (and print a warning to the logs).  If you have a certificate,
install it in the files `data/cert.pem` and `data/key.pem`:

```sh
ssh galene@galene.example.org
sudo cp /etc/letsencrypt/live/galene.example.org/fullchain.pem data/cert.pem
sudo cp /etc/letsencrypt/live/galene.example.org/privkey.pem data/key.pem
sudo chown galene:galene data/*.pem
chmod go-rw data/key.pem
```

Since certificates are regularly rotated, this should be done in a monthly
cron job (or a *SystemD* timer unit, if you're feeling particularly kinky).

### Run Galene on the server

Arrange to run the binary on the server.  If you never reboot your server,
just do:

```sh
ssh galene@galene.example.org
ulimit -n 65536
nohup ./galene &
```

If you are using *runit*, use a script like the following:

```sh
#!/bin/sh
exec 2>&1
cd ~galene
ulimit -n 65536
exec setuidgid galene ./galene
```

If you are using *SystemD*, put the following in
`/etc/systemd/system/galene.service` (and then run `systemctl daemon-reload`):

```ini
[Unit]
Description=Galene
After=network.target

[Service]
Type=simple
WorkingDirectory=/home/galene
User=galene
Group=galene
ExecStart=/home/galene/galene
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

### Set up galenectl

There are two ways to administer a Galene instance: by manually editing
JSON files on the server, or by using the `galenectl` utility.
The `galenectl` utility is recommended, since it avoids issues with
concurrent modifications and is less error-prone than the alternative.

Build the `galenectl` utility, and copy it somewhere on your path:

```sh
cd galenectl
go build -ldflags='-s -w'
sudo cp galenectl /usr/local/bin/
```

Now create an administrator password, and set up galenectl:

```sh
galenectl -admin-username admin initial-setup
```

This command creates two files: `galenectl.json` and `config.json`.  The
former is already at the right place, the latter must be copied to the
server's `data/` directory:

```sh
rsync config.json galene@galene.example.org:data/
```

### Group setup

Create a group:

```sh
galenectl create-group -group city-watch
```

If you didn't install a TLS certificate above, you will need to run
`galenectl` with the flag `-insecure`:

```sh
galenectl -insecure create-group -group city-watch
```

Create an "op", a user with group moderation privileges:

```sh
galenectl create-user -group city-watch -user vimes -permissions op
```

Set the new user's password:

```sh
galenectl set-password -group city-watch -user vimes
```

You should now be able to test your Galene installation by pointing a web
browser at <https://galene.example.org:8443/group/city-watch/>.

Create an ordinary user:

```sh
galenectl create-user -group city-watch -user fred
galenectl set-password -group city-watch -user fred
```

Check the results:

```sh
galenectl list-groups
galenectl list-users -l -group city-watch
```

Type `galenectl -help`, `galenectl create-group -help`, etc. for more
information.

## Advanced configuration

Galene is designed to be exposed directly to the Internet.  If your server
is behind a firewall or NAT router, some extra configuration is necessary.

### Running behind a firewall

If your server is behind a firewall but has a global IPv4 address (it is
not behind NAT), then, at the very minimum, the firewall must allow
incoming connections to:

  * TCP port 8443 (or whatever is configured with the `-http` option); and

  * TCP and UDP port 1194 (or whatever is configured with the `-turn` option).

For good performance, your firewall should allow incoming and outgoing
traffic from the UDP ports used for media transfer.  By default, these are
all high-numbered (ephemeral) ports, but they can be restricted using one
of the following options:

  * the `-udp-range port1-port2` option restricts the UDP ports to be in
    the range from port1 to port2 inclusive; this should be a large range,
    on the order of a few tens of thousands of ports;

  * the `-udp-range port` option makes the server use just a single port,
    and demultiplex the traffic in userspace.

At the time of writing, this mechanism is not quite complete, and you will
see Galene attempting to use other ports.  Unless you see connection
failures, this is nothing to worry about.

### Running behind NAT

If your server is behind NAT, then currently the only option is to use
a STUN, or, preferably, TURN server on a separate host, one that is not
behind NAT.  See Section *Connectivity issues and ICE servers* below.

Galene has some support for running behind NAT without a helpful server,
but this has not been exhaustively tested.  Please see the section
"Connectivity issues and ICE servers" below.

### Running behind a reverse proxy

Galene is designed to be directly exposed to the Internet.  In order to
run Galene behind a reverse proxy, you might need to make a number of
tweaks to your configuration.

First, you might need to inform Galene of the URL at which users connect
(the reverse proxy's URL) by adding an entry `proxyURL` to your
`data/config.json` file:

```json
{
    "proxyURL": "https://galene.example.org/"
}
```

Second, and depending on your proxy implementation, you might need to
request that the proxy pass WebSocket handshakes to the URL at `ws`; for
example, with Nginx, you will need to say something like the following:

```
location /ws {
    proxy_pass https://localhost:8443/ws;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "Upgrade";
    proxy_set_header Host $http_host;
}
```

Finally, in order to avoid TLS termination issues, you may want to run
Galene over plain HTTP instead of HTTPS by using the command-line flag
`-insecure`.

Note that even if you're using a reverse proxy, clients will attempt to
establish direct UDP flows with Galene and direct TCP connections to
Galene's TURN server; see the section *Configuring your firewall*
above.

## Connectivity issues and ICE servers

Most connectivity issues are due to an incorrect ICE configuration.

ICE is the NAT and firewall traversal protocol used by WebRTC.  ICE can
make use of two kinds of servers to help with NAT traversal: STUN servers,
that help punching holes in well-behaved NATs, and TURN servers, that
serve as relays for traffic.  TURN is a superset of STUN: no STUN server
is necessary if one or more TURN servers are available.

Galene includes an IPv4-only TURN server, which is controlled by the
`-turn` command-line option.  It has the following behaviour:

  * if its value is set to the empty string `""`, then the built-in server
    is disabled; in this case, the file `data/ice-servers.json` configures
    an external TURN server;

  * if its value is a colon followed with a port number, for example
    `:1194`, then the TURN server will listen on all public IPv4 addresses
    of the local host, over UDP and TCP; this is the recommended value if
    the server is not behind NAT, and the firewall allows incoming
    connections to the TURN port.

  * if the value of this option is a socket address, such as
    `203.0.113.1:1194`, then the TURN server will listen on all addresses
    of the local host but assume that the address seen by the clients is
    the one given in the option; this may be useful when running behind NAT
    with port forwarding set up.

  * the default value is `auto`, which behaves like `:1194` if there is no
    `data/ice-servers.json` file, and like `""` otherwise.

If the server is not accessible from the Internet, e.g. because of NAT or
because it is behind a restrictive firewall, then you should configure
a TURN server that runs on a host that is accessible by both Galene and
the clients.  Disable the built-in TURN server (`-turn ""` or the default
`-turn auto`), and provide a working ICE configuration in the file
`data/ice-servers.json`.  In the case of a single STUN server, it should
look like this:

```json
[
    {
        "urls": [
            "stun:stun.example.org"
        ]
    }
]
```

In the case of a single TURN server, the `ice-servers.json` file should
look like this:

```json
[
    {
        "urls": [
            "turn:turn.example.org:443",
            "turn:turn.example.org:443?transport=tcp"
        ],
        "username": "galene",
        "credential": "secret"
    }
]
```

It is more secure to use coturn's `use-auth-secret` option.  If you do
that, then the `ice-servers.json` file should look like this:

```json
[
    {
        "urls": [
            "turn:turn.example.org:443",
            "turn:turn.example.org:443?transport=tcp"
        ],
        "username": "galene",
        "credential": "secret",
        "credentialType": "hmac-sha1"
    }
]
```

For redundancy, you may set up multiple TURN servers, and ICE will use the
first one that works.  If an `ice-servers.json` file is present and
Galene's built-in TURN server is enabled, then the external server will be
used in preference to the built-in server.
