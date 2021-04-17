# Installation

## Build the `galene` binary

You will need Go 1.13 or later (type `go version`).  Then do:

    CGO_ENABLED=0 go build -ldflags='-s -w'

## Set the server administrator credentials

This step is optional.

    mkdir data
    echo 'god:topsecret' > data/passwd

## Set up a group

A group called *groupname* is is set up by creating a file
`groups/groupname.json`.

    mkdir groups
    vi groups/groupname.json
    
A group with a single operator and no password for ordinary users looks
like this:

    {
        "op": [{"username": "jch", "password": "1234"}],
        "presenter": [{}]
    }
   
A group with one operator and two users looks like this:

    {
        "op": [{"username": "jch", "password": "1234"}],
        "presenter": [
            {"username": "mom", "password": "0000"},
            {"username": "dad", "password": "1234"}
        ]
    }
    
More options are described under *Details of group definitions* below.

## Test locally

    ./galene &
    
You should be able to access Galène at `https://localhost:8443`.  Connect
to the group that you have just set up in two distinct browser windows,
then press *Ready* in one of the two; you should see a video in the other.

If you have set up a TURN server, type `/relay-test` in the chat box; if
the TURN server is properly configured, you should see a message saying
that the relay test has been successful.  (The relay test will fail if you
didn't configure a TURN server; this is normal, and nothing to worry
about.)

## Configure your server's firewall

If your server has a global IPv4 address and there is no firewall, there
is nothing to do.

If your server has a global IPv4 address, then the firewall must, at
a strict minimum, allow incoming traffic to TCP port 8443 (or whatever is
configured with the `-http` command-line option) and TCP port 1194 (or
whatever is configured with the `-turn` command-line option).  For best
performance, it should also allow UDP traffic to the TURN port, and UDP
traffic to ephemeral (high-numbered) ports.

If your server only has a global IPv6 address, then you should probably
configure an external double-stack (IPv4 and IPv6) TURN server: see
"ICE Servers" below.

If your server is behind NAT, then the best solution is to run an external
TURN server that is not behind NAT (see "ICE Servers" below).  If that is
not possible, then you should configure your NAT device to forward, at
a minimum, ports 8443 (TCP) and 1194 (TCP and UDP).  In addition, you
should add the option `-turn 203.0.113.1:1194` to Galène's command line,
where `203.0.113.1` is your NAT's external (global) IPv4 address.

## Cross-compile for your server

This step is only required if your server runs a different OS or has
a different CPU than your build machine.

For a Linux server with an Intel or AMD CPU:

    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w'

For a Raspberry Pi 1:

    CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -ldflags='-s -w'

For a BeagleBone or a Raspberry Pi 2 or later:

    CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags='-s -w'

For a 64-bit ARM board (Olimex Olinuxino-A64, Pine64, etc.):

    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags='-s -w'

For a 32-bit MIPS board with no hardware floating point (WNDR3800, etc.):

    CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat go build -ldflags='-s -w'

## Deploy to your server

Set up a user *galene* on your server, then do:

    rsync -a galene static data groups galene@server.example.org:

If you don't have a TLS certificate, Galène will generate a self-signed
certificate automatically (and print a warning to the logs).  If you have
a certificate, install it in the files `data/cert.pem` and `data/key.pem`:

    ssh galene@server.example.org
    sudo cp /etc/letsencrypt/live/server.example.org/fullchain.pem data/cert.pem
    sudo cp /etc/letsencrypt/live/server.example.org/key.pem data/key.pem
    sudo chown galene:galene data/*.pem
    sudo chmod go-rw data/key.pem
    
Now run the binary on the server:

    ssh galene@server.example.org
    ulimit -n 65536
    nohup ./galene &

If you are using *runit*, use a script like the following:

    #!/bin/sh
    exec 2>&1
    cd ~galene
    ulimit -n 65536
    exec setuidgid galene ./galene

If you are using *systemd*:

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

# Usage

## Locations

There is a landing page at the root of the server.  It contains a form
for typing the name of a group, and a clickable list of public groups.

Groups are available under `/group/groupname`.  You may share this URL
with others, there is no need to go through the landing page.

Recordings can be accessed under `/recordings/groupname`.  This is only
available to the administrator of the group.

Some statistics are available under `/stats`.  This is only available to
the server administrator.

## Side menu

There is a menu on the right of the user interface.  This allows choosing
the camera and microphone and setting the video throughput.  The
*Blackboard mode* checkbox increases resolution and sacrifices framerate
in favour of image quality.  The *Play local file* dialog allows streaming
a video from a local file.

## Commands

Typing a line starting with a slash `/` in the chat dialogue causes
a command to be sent to the server.  Type `/help` to get the list of
available commands; the output depends on whether you are an operator or
not.


# Details of group definitions

Groups are defined by files in the `./groups` directory (this may be
configured by the `-groups` command-line option, try `./galene -help`).
The definition for the group called *groupname* is in the file
`groups/groupname.json`; it does not contain the group name, which makes
it easy to copy or link group definitions.  You may use subdirectories:
a file `groups/teaching/networking.json` defines a group called
*teching/networking*.

Every group definition file contains a JSON directory.  All fields are
optional, but unless you specify at least one user definition (`op`,
`presenter`, or `other`), nobody will be able to join the group.  The
following fields are allowed:

 - `op`, `presenter`, `other`: each of these is an array of user
   definitions (see below) and specifies the users allowed to connect
   respectively with operator privileges, with presenter privileges, and
   as passive listeners;
 - `public`: if true, then the group is visible on the landing page;
 - `description`: a human-readable description of the group; this is
   displayed on the landing page for public groups;
 - `contact`: a human-readable contact for this group, such as an e-mail
   address;
 - `comment`: a human-readable string;
 - `max-clients`: the maximum number of clients that may join the group at
   a time;
 - `max-history-age`: the time, in seconds, during which chat history is
   kept (default 14400, i.e. 4 hours);
 - `allow-recording`: if true, then recording is allowed in this group;
 - `allow-anonymous`: if true, then users may connect with an empty username;
 - `allow-subgroups`: if true, then subgroups of the form `group/subgroup`
   are automatically created when first accessed;
 - `autolock`: if true, the group will start locked and become locked
   whenever there are no clients with operator privileges;
 - `autokick`: if true, all clients will be kicked out whenever there are
   no clients with operator privileges; this is not recommended, prefer
   the `autolock` option instead;
 - `redirect`: if set, then attempts to join the group will be redirected
   to the given URL; most other fields are ignored in this case;
 - `codecs`: this is a list of codecs allowed in this group.  The default
   is `["vp8", "opus"]`.
   
Supported video codecs include:

 - `"vp8"` (compatible with all supported browsers);
 - `"vp9"` (better video quality than `"vp8"`, but incompatible with
   older versions of Mac OS);
 - `"h264"` (incompatible with Debian, Ubuntu, and some Android devices,
   recording is not supported).

Supported audio codecs include `"opus"`, `"g722"`, `"pcmu"` and `"pcma"`.
There is no good reason to use anything except Opus.
   
A user definition is a dictionary with the following fields:

 - `username`: the username of the user; if omitted, any username is
   allowed;
 - `password`: if omitted, then no password is required.  Otherwise, this
   can either be a string, specifying a plain text password, or
   a dictionary generated by the `galene-password-generator` utility.
   
For example,

    {"username": "jch", "password": "1234"}
    
specifies user *jch* with password *1234*, while

    {"password": "1234"}
    
specifies that any (non-empty) username will do, and

    {}
    
allows any (non-empty) username with any password.

If you don't wish to store cleartext passwords on the server, you may
generate hashed password with the `galene-password-generator` utility.  A
user entry with a hashed password looks like this:

    {
        "username": "jch",
        "password": {
            "type": "pbkdf2",
            "hash": "sha-256",
            "key": "f591c35604e6aef572851d9c3543c812566b032b6dc083c81edd15cc24449913",
            "salt": "92bff2ace56fe38f",
            "iterations": 4096
        }
    }

# ICE Servers

ICE is the NAT and firewall traversal protocol used by WebRTC.  ICE can
make use of two kinds of servers to help with NAT traversal: STUN servers,
that help punching holes in well-behaved NATs, and TURN servers, that
serve as relays for traffic.  TURN is a superset of STUN: no STUN server
is necessary if a TURN server is available.

Galène includes an IPv4-only TURN server, which is controlled by the
`-turn` command-line option.  If its value is set to the empty string
`""`, then the built-in server is disabled.  If its value is a colon
followed with a port number, for example `:1194`, then the TURN server
will listen on all public IPv4 addresses of the local host, over UDP and
TCP.  If the value of this option is a socket address, such as
`203.0.113.1:1194`, then the TURN server will listen on all addresses of
the local host but assume that the address seen by the clients is the one
given in the option; this is useful when running behind NAT with port
forwarding set up.  The default value is `-turn auto`, which starts a
TURN server on port 1194 unless there is a `data/ice-servers.json` file.

Some users may prefer to use an external ICE server.  In that case, the
built-in TURN server should be disabled (`-turn ""` or the default `-turn
auto`), and a working ICE configuration should be given in the file
`data/ice-servers.json`.  In the case of a single STUN server, it should
look like this:

    [
        {
            "urls": [
                "stun:stun.example.org"
            ]
        }
    ]
    
In the case of s single TURN server, the `ice-servers.json` file should
look like this:

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

If you prefer to use coturn's `use-auth-secret` option, then the
`ice-servers.json` file should look like this:

    [
        {
            "Urls": [
                "turn:turn.example.com:443",
                "turn:turn.example.com:443?transport=tcp"
            ],
            "username": "galene",
            "credential": "secret",
            "credentialType": "hmac-sha1"
        }
    ]
    
For redundancy, you may set up multiple TURN servers, and ICE will use the
first one that works.  If an `ice-servers.json` file is present and
Galène's built-in TURN server is enabled, then the external server will be
used in preference to the built-in server.

# Further information

Galène's web page is at <https://galene.org>.

Answers to common questions and issues are at <https://galene.org#faq>.

-- Juliusz Chroboczek <https://www.irif.fr/~jch/>
