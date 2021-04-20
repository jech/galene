# Installation

## Build the `galene` binary

You will need Go 1.13 or later (type `go version`).  Then do:

	cd admin
    CGO_ENABLED=0 go build -ldflags='-s -w'

## Set up an administrator

Create a file `admin/admin.json`:

	vi admin/admin.json

And write your admin's usernames and passwords:

	{
		"admin" : [{"username": "admin", "password": "1234"}]
	}

You can have several admin account, you can have an empty username or/and password

## Test locally

	./admin/admin

You should be able to access to the admin interface of Galène at `https://localhost:8444`

For more information consult the Galène README
