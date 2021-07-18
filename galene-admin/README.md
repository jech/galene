# Installation

## Build the `galene-admin` binary

You will need Go 1.13 or later (type `go version`).  Then do:

	cd galene-admin
	CGO_ENABLED=0 go build -ldflags='-s -w'

## Set up an administrator

Create a file `galene-admin/admin.json`:

	vi galene-admin/admin.json

And write your admin's usernames and passwords:

	{
		"admin" : [{"username": "admin", "password": "1234"}]
	}

You can have several admin account, you can have an empty username or/and password

## Test locally

Please make sure you are in the galene-admin directory before launch the application

	cd galene-admin
	./galene-admin

You should be able to access to the admin interface of Galène at `https://localhost:8444`

If you want to use galene-admin on a different directory than Galene you can specify by passing the path as a parameter:

	./galene-admin path

By default the path is `../`

For more information consult the Galène README
