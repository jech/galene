# Galène videoconferencing server

Galène is a videoconferencing server that is easy to deploy (just copy a few files and run the binary) and that requires moderate server resources. It was originally designed for lectures and conferences (where a single speaker streams audio and video to hundreds or thousands of users), but later evolved to be useful for student practicals (where users are divided into many small groups), and meetings (where a few dozen users interact with each other).

Galène's server side is implemented in Go, and uses the Pion implementation of WebRTC. The server has been tested on Linux/amd64 and Linux/arm64, and should in principle be portable to other systems (including Mac OS X and Windows). The client is implemented in Javascript, and works on recent versions of all major web browsers, both on desktop and mobile.

## Setup

The setup requires Docker. Checkout the Development section in case
you would like to have a bit more control and build Galène yourself.

```bash
docker run -it -p 8443:8443 garage44/galene:latest
```

* Open a compatible browser to the [Galène frontend](http://localhost:8443)

:tada: You're now running Galène locally.

> Please note that you may need a slightly more extended setup when you
> want to have conferences between multiple users.

## Development

With a local Golang environment, build Galène manually with:

```bash
git clone git@github.com:garage44/galene.git
cd galene
CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w'
```

## Documentation

Checkout [the wiki](https://github.com/garage44/galene/wiki) for further setup
and usage instructions.