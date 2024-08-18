Galene is a videoconferencing server that is easy to deploy and requires
moderate server resources.  It is described at <https://galene.org>.


# Installation

See the file INSTALL in this directory for installation instructions.


# Usage

## Locations

There is a landing page at the root of the server.  It contains a form
for typing the name of a group, and a clickable list of public groups.

Groups are available under `/group/groupname/`.  You may share this URL
with others, there is no need to go through the landing page.

Recordings can be accessed under `/recordings/groupname/`.  This is only
available to the administrator of the group.

Some statistics are available under `/stats.json`, with a human-readable
version at `/stats.html`.  This is only available to the server administrator.


## Main interface

After logging in, the user is confronted with the main interface.

### Buttons

There are up to three buttons at the top.  The *Enable*/*Disable* button
enables either or both the camera and the microphone (depending on the
options set in the side menu, see below).  The *Mute* button mutes or
unmutes the microphone.  The *Share Screen* button shares the screen or
a window.

### Side menu

There is a menu on the right of the user interface.  This allows choosing
the camera and microphone and setting the video throughput.  The
*Blackboard mode* checkbox increases resolution and sacrifices framerate
in favour of image quality.  The *Play local file* dialog allows streaming
a video from a local file.

### User list

There is a user list on the left.  Clicking on a user opens a menu with
actions that can be applied to that user.  Clicking on ones own username
opens a menu with actions that are global to the group.

### Chat pane

Double-clicking on a message opens a contextual menu.

### Text box

Typing a string in the text box at the bottom of the chat pane sends
a broadcast message to all of the users in the group.

Typing a line starting with a slash `/` in the text box causes a command
to be sent to the server.  Type `/help` to get the list of available
commands; the output depends on whether you are an operator or not.


# The global configuration file

The server may be configured in the JSON file `data/config.json`.  This
file may look as follows:

    {
        "users":{"root": {"password":"secret", "permissions": "admin"}},
        "canonicalHost": "galene.example.org"
    }

The fields are as follows:

- `users` defines the users allowed to administer the server, and has the
  same syntax as user definitions in groups (see below), except that the
  only meaningful permission is `"admin"`;
- `writableGroups`: if true, then the API can modify group description
  files; by default, group files are treated as read-only;
- `publicServer`: if true, then cross-origin access to the server is
  allowed.  This is safe if the server is on the public Internet, but not
  necessarily so if it is on a private network.
- `proxyURL`: if running behind a reverse proxy, this specifies the
  root URL that will be visible outside the proxy.
- `canonicalHost`: the canonical name of the host running the server; this
  will cause clients to be redirected if they use a different hostname to
  access the server.


# Group definitions

Groups are defined by files in the `./groups` directory (this may be
configured by the `-groups` command-line option, try `./galene -help`).
The definition for the group called *groupname* is in the file
`groups/groupname.json`; it does not contain the group name, which makes
it easy to copy or link group definitions.  You may use subdirectories:
a file `groups/teaching/networking.json` defines a group called
*teaching/networking*.


## Examples

A typical group definition file looks like this:

    {
        "users":{
            "jch": {"password":"1234", "permissions": "op"}
        },
        "allow-recording": true,
        "auto-subgroups": true
    }

This defines a group with the operator (administrator) username *jch* and
password *1234*.  The `allow-recording` entry says that the operator is
allowed to record videos to disk, and the `auto-subgroups` entry says
that subgroups will be created automatically.  This particular group does
not allow password login for ordinary users, and is suitable if you use
invitations (see *Stateful Tokens* below) for ordinary users.

In order to allow password login for ordinary users, add password entries
with the permission `present`:

    {
        "users":{
            "jch":  {"password": "1234", "permissions": "op"}
            "john": {"password": "secret", "permissions": "present"}
        }
    }

If the group is to be publicly accessible, you may allow logins with any
username using the `wildcard-user` entry::

    {
        "users":{
            "jch": {"password":"1234", "permissions": "op"}
        },
        "wildcard-user": {"password": "1234", "permissions": "present"},
        "public": true
    }

If you want to allow users to use any password, use a wildcard password:

    {
        "users":{
            "jch": {"password":"1234", "permissions": "op"}
        },
        "wildcard-user":
            {"password": {"type": "wildcard"}, "permissions": "present"},
        "public": true
    }

## Reference

Every group definition file contains a single JSON directory (a list of
entries between `{' and `}').  All fields are optional, but unless you
specify at least one user definition (`op`, `presenter`, or `other`),
nobody will be able to join the group.  The following fields are allowed:

 - `users`: is a dictionary that maps user names to dictionaries with
   entries `password` and `permissions`; `permissions` should be one of
   `op`, `present`, `message` or `observe`.
 - `wildcard-user` is a dictionaries with entries `password` and `permissions`
   that will be used for usernames with no matching entry in the `users`
   dictionary;
 - `authKeys`, `authServer` and `authPortal`: see *Authorisation* below;
 - `public`: if true, then the group is listed on the landing page;
 - `displayName`: a human-friendly version of the group name;
 - `description`: a human-readable description of the group; this is
   displayed on the landing page for public groups;
 - `contact`: a human-readable contact for this group, such as an e-mail
   address;
 - `comment`: a human-readable string;
 - `max-clients`: the maximum number of clients that may join the group at
   a time;
 - `max-history-age`: the time, in seconds, during which chat history is
   kept (default 14400, i.e. 4 hours);
 - `not-before` and `expires`: the times (in ISO 8601 or RFC 3339 format)
   between which joining the group is allowed;
 - `allow-recording`: if true, then recording is allowed in this group;
 - `unrestricted-tokens`: if true, then ordinary users (without the "op"
   privilege) are allowed to create tokens;
 - `allow-anonymous`: if true, then users may connect with an empty username;
 - `auto-subgroups`: if true, then subgroups of the form `group/subgroup`
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
 - `"vp9"` (better video quality, but incompatible with Safari);
 - `"av1"` (even better video quality, only supported by some browsers,
   recording is not supported, SVC is not supported);
 - `"h264"` (incompatible with Debian and with some Android devices, SVC
   is not supported).

Supported audio codecs include `"opus"`, `"g722"`, `"pcmu"` and `"pcma"`.
Only Opus can be recorded to disk.  There is no good reason to use
anything except Opus.


## Client Authorisation

Galene implements three authorisation methods: a simple username/password
authorisation scheme, a scheme using stateful tokens and a mechanism based
on cryptographic tokens that are generated by an external server.  The
former two mechanism are intended to be used in standalone installations,
while the server-based mechanism is designed to allow easy integration
with an existing authorisation infrastructure (such as LDAP, OAuth2, or
even Unix passwords).

### Password authorisation

When password authorisation is used, authorised usernames and password are
defined directly in the group configuration file, in the `users` and
`fallback-users` entries.  The `users` entry is a dictionary that maps
user names to user descriptions; the `fallback-users` is a list of user
descriptions that are used with usernames that don't appear in `users`.

Every user description is a dictionary with fields `password` and
`permissions`.  The `password` field may be a literal password string, or
a dictionary describing a hashed password or a wildcard.  The
`permissions` field should be one of `op`, `present`, `message` or
`observe`.  (An array of Galene's internal permissions is also allowed,
but this is not recommended, since internal permissions may vary from
version to version).

For example, the entry

    "users": {"jch": {"password": "1234", "permissions": "op"}}
    
specifies that user "jch" may login as operator with password "1234", while

    "fallback-users": [{"password": "1234", "permissions": "present"}]
    
allows any username with password *1234*.  Finally,

    "fallback-users": [
        {"password": {"type": "wildcard"}, "permissions": "present"}
    ]
    
allows any username with any password.


### Hashed passwords

If you don't wish to store cleartext passwords on the server, you may
generate hashed passwords with the `galene-password-generator` utility.  A
user entry with a hashed password looks like this:

    "users": {
        "jch": {
            "password": {
                "type": "pbkdf2",
                "hash": "sha-256",
                "key": "f591c35604e6aef572851d9c3543c812566b032b6dc083c81edd15cc24449913",
                "salt": "92bff2ace56fe38f",
                "iterations": 4096
            },
            "permissions": "op"
        }
    }


### Stateful tokens

Stateful tokens allow to temporarily grant access to a user.  In order to
generate a stateful token, the group administrator types

    /invite user period

where `user` is the username granted to the temporary user, and `period`
is the time period for which the token will be valid (for example `2d`
meaning 2 days).  The server replies with a link, valid the given time
period, that may be sent to the temporary user for example by e-mail.

Tokens may also be granted without imposing a specific username:

    /invite '' 2d

Stateful tokens are revokable (use the `/revoke` command) and their
lifetime may be extended (use the `/reinvite` command).


### Authorisation servers

Galene is able to delegate authorisation decisions to an external
authorisation server.  This makes it possible to integrate Galene with an
existing authentication and authorisation infrastructure, such as LDAP,
OAuth2 or even Unix passwords.

When an authorisation server is used, the group configuration file
specifies one or more public keys in JWK format (with the restriction that
the "alg" key must be specified).  In addition, it may specify either an
authorisation server or an authorisation portal.

    {
        "authKeys": [{
            "kty": "oct",
            "alg": "HS256",
            "k": "MYz3IfCq4Yq-UmPdNqWEOdPl4C_m9imHHs9uveDUJGQ",
        }, {
            "kty": "EC",
            "alg": "ES256",
            "crv": "P-256",
            "x": "dElK9qBNyCpRXdvJsn4GdjrFzScSzpkz_I0JhKbYC88",
            "y": "pBhVb37haKvwEoleoW3qxnT4y5bK35_RTP7_RmFKR6Q",
        }]
        "authServer": "https://auth.example.org",
    }

If multiple keys are provided, then they will all be tried in turn, unless
the token includes the "kid" header field, in which case only the
specified key will be used.

If an authorisation server is specified, then the default client, after it
prompts for a password, will request a token from the authorisation server
and will join the group using token authentication.  The password is never
communicated to the server.

If an authorisation portal is specified, then the default client will
redirect initial client connections to the authorisation portal.  The
authorisation portal is expected to authorise the client and then redirect
it to Galene with the `username` and `token` query parameters set.


# Further information

Galène's web page is at <https://galene.org>.

Answers to common questions and issues are at <https://galene.org/faq.html>.


-- Juliusz Chroboczek <https://www.irif.fr/~jch/>
