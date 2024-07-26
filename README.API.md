# Galene's administrative API

Galene provides an HTTP-based API that can be used to create groups and
users.  For example, in order to create a group, a client may do

    PUT /galene-api/v0/.groups/groupname/
    Content-Type: application/json
    If-None-Match: *

The `If-None-Match` header avoids overwriting an existing group.

In order to edit a group definition, a client first does

    GET /galene-api/v0/.groups/groupname/

This yields the group definition and an entity tag (in the ETag header).
The client then modifies the group defintion, and does

    PUT /galene-api/v0/.groups/groupname/
    If-Match: "abcd"

where "abcd" is the entity tag returned by the GET request.  If the group
definition has changed in the meantime, the entity tag will no longer be
valid, and the server will fail the update, which avoids losing an update
in the case of a concurrent modification.


## Endpoints

The API is located under `/galene-api/v0/`.  The `/v0/` is a version number,
and will be incremented if we ever find out that the current API cannot be
extended in a backwards compatible manner.

### Statistics

    /galene-api/v0/.stats

Provides a number of statistics about the running server, in JSON.  The
exact format is undocumented, and may change between versions.  The only
allowed methods are HEAD and GET.

### List of groups

    /galene-api/v0/.groups/

Returns a list of groups, as a JSON array.  The only allowed methods are
HEAD and GET.

### Group definition

    /galene-api/v0/.groups/groupname

Contains a "sanitised" group definition in JSON format, analogous to the
on-disk format but without any user definitions or cryptographic keys.
Allowed methods are HEAD, GET, PUT and DELETE.  The only accepted
content-type is `application/json`.

### Authentication keys

    /galene-api/v0/.groups/groupname/.keys

Contains the keys used for validation of stateless tokens, encoded as
a JSON key set (RFC 7517).  Allowed methods are PUT and DELETE.  The only
accepted content-type is `application/jwk-set+json`.

### List of users

    /galene-api/v0/.groups/groupname/.users/

Returns a list of users, as a JSON array.  The only allowed methods are
HEAD and GET.

### User definitions

    /galene-api/v0/.groups/groupname/.users/username
    /galene-api/v0/.groups/groupname/.empty-user
    /galene-api/v0/.groups/groupname/.wildcard-user

Contains a "sanitised" user definition (without any passwords), a JSON
object with a single field `permissions`.  The entries `.empty-user` and
`.wildcard-user` are for the user with the empty username and the wildcard
user respectively.  Allowed methods are HEAD, GET, PUT and DELETE.  The
only accepted content-type is `application/json`.

### Passwords

    /galene-api/v0/.groups/groupname/.users/username/.password
    /galene-api/v0/.groups/groupname/.empty-user/.password
    /galene-api/v0/.groups/groupname/.wildcard-user/.password

Contains the password of a given user.  The PUT method takes a full
password definition, identical to what can appear in the `"password"`
field of the on-disk format, while the POST method takes a string which
will be hashed on the server.  Allowed methods are PUT, POST and DELETE.
Accepted content-types are `application/json` for PUT and `text/plain` for
POST.

### Wildcard user

    /galene-api/v0/.groups/groupname/.wildcard-user

Contains a dictionary defining the wildcard user, in the same format as
the dictionary defining an ordinary user.  Allowed methods are HEAD, GET,
PUT and DELETE.

### Wildcard user password

    /galene-api/v0/.groups/groupname/.wildcard-user/.password

This is analogous to the password of an ordinary user.  Allowed methods
are PUT, POST and DELETE.

### List of stateful tokens

    /galene-api/v0/.groups/groupname/.users/username/.tokens/

GET returns the list of stateful tokens, as a JSON array.  POST creates
a new token, and returns its name in the `Location` header.  Allowed
methods are HEAD, GET and POST.

### Stateful token

    /galene-api/v0/.groups/groupname/.users/username/.tokens/token

The full contents of a single token, in JSON.  The exact format may change
between versions, so a client should first GET a token, update one or more
fields, then PUT the resulting token.  Allowed methods are HEAD, GET and
PUT.
