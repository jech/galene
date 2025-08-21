# Galene manual

Please see the file [galene-install.md][1] for installation instructions.
Please see the section "Server administration" below for detailed
administration instructions.

## Usage

Galene includes a web server that, by default, listens on port 8443.
Galene is therefore accessed on URLs such as:

    https://galene.example.org:8443/

### The landing page

There is a landing page at the root of the server.  The landing page
contains a list of all groups marked "public" in their configuration, as
well as a form that allows joining an arbitrary group.

Going through the landing page is not required: you are welcome to point
your browser directly at the desired group URL.

### The group pages

A group named e.g. *city-watch* has an associated page
`/group/city-watch/`.  After login, it presents an interface consisting
of three panes:

  - the left side pane contains the list of users who have joined the
    group; every username doubles as a menu of group and user actions;
  - the middle pane contains the chat messages published to the group;
  - the main pane, on the right, contains the videos being streamed by
    users in the group.

On mobile, only the latter pane is shown by default.  An icon at the top
left opens the user list, and an icon in the main pane switches to the
chat.

### Buttons

There are up to three buttons at the top.  The most important is The
*Enable*/*Disable* button, which switches on or off both the camera and
the microphone.

The *Mute* button mutes or unmutes the microphone; the microphone can be
muted remotely by the group moderator, but it cannot be unmuted remotely.

The *Share screen* button streams the contents of the screen or an
individual window.

### Side menu

There is a menu on the right of the user interface.  It allows switching
off the camera (for audio-only sessions), choosing the camera and
microphone and setting the video throughput.  The *Blackboard mode*
checkbox increases resolution and sacrifices framerate in favour of image
quality.  The *Play local file* dialog streams a video from a local file.

### User list

There is a user list on the left, starting with the current user and
continuing with all users that have joined the current group.

The user list doubles as a set of menus.  Clicking on the current user (the
first entry in the user list) opens the *group menu*, a menu with actions
that apply to the group as a whole.  Clicking on a different user opens
a *user menu*, a menu that applies to that specific user.

### Chat pane

The center pane is a traditional chat interface, with an input form at the
bottom and the chat history above it.  Chat history is never saved to
disk, and is erased after four hours (or whatever is specified in the
`"max-history-age"` field of the group definition).

Double-clicking on a message opens a contextual menu.

The chat form doubles as a command-line interface, which is especially
important for visually-impaired users, and more generally is often faster
than navigating the user interface.  Commands start with a slash character
"`/`".  The most important command is `/msg`, which sends a private
message to a given user.  Type `/help` to display the list of available
commands.

### Inviting users

In order to generate an invitation link, choose the entry "Invite user" in
the group menu.  This generates a link of the form

    https://galene.example.org:8443/group/city-watch/?token=XXX

where the *XXX* part, known as the *token*, is a shared secret. Such
a link allows password-less login to the group, and may therefore be
shared e.g. over e-mail or instant messaging.

The invitation functionality is usually restricted to the moderator;
however, groups may be configured with the `"unrestricted-tokens"` option,
which allows all users to generate tokens.

Tokens can be created, modified, and expired using the `/invite`,
`/reinvite` and `/revoke` commands.

### File transfer

Galene includes a peer-to-peer, end-to-end encrypted file transfer protocol.
In order to transfer a file, click on the receiver's entry in the user
list and choose "Send file".

### Group moderation

If a user has the *op* permission (short for *Operator*), then they have
access to a number of moderation tools.

For a moderator, the contextual menu that opens when clicking on an entry
in the user list is expanded with commands for muting a user, sending them
a warning, retrieving their IP address, or kicking them out from a group.

The group menu (opened by clicking on one's own entry in the user's list)
is extended with options to lock or to unlock a group (a locked group is
one that non-operator users cannot join).

All of the moderation commands are also available as command-line commands
(see above), which is helpful when moderating large groups.

# Server administration


## The global configuration file

The server may be configured in the JSON file `data/config.json`.  This
file may look as follows:

```json
{
    "users":{"vetinari": {"password":"lagniappe", "permissions": "admin"}},
    "canonicalHost": "galene.example.org",
    "writableGroups": true
}
```

or, better, with a hashed password:

```json
{
    "users": {
        "vetinari": {
            "password":{"type":"bcrypt","key":"$2a$10$bTWW..."},
            "permissions": "admin"
        }
    },
    "canonicalHost": "galene.example.org",
    "writableGroups": true
}
```

The file is initially set up using `galenectl initial-setup`, but may be
manually edited at any time (there is no need to restart the server).  The
fields are as follows:

 - `users` defines the users allowed to administer the server, and has the
   same syntax as user definitions in groups (see below), except that the
   only meaningful permission is `"admin"`;

 - `writableGroups`: if true, then the API used by `galenectl` can be used
   to modify group definitions; if unset or false, then only read-only
   access is allowed;

 - `allowOrigin` is an array that contains the list of HTTP origins that
   are allowed to access the server;

 - `allowAdminOrigin` is like `allowOrigin`, but applies to the
   administrative API (the one used by `galenectl`);

 - `proxyURL`: if running behind a reverse proxy, this specifies the root
   URL that will be visible outside the proxy;

 - `canonicalHost`: the canonical name of the host running the server;
   clients that attempt to access the server using a different host name
   will be redirected to the canonical one.


## Group definitions

Groups are described by JSON files in the `./groups/` directory.  These
files are normally administered using the `galenectl` utility, but may
also be edited manually (there is no need to restart the server).

### Managing groups using `galenectl`

#### Creating, modifying, and deleting groups

A group is created using `galenectl create-group`:

```sh
galenectl create-group -group city-watch
```

There are a number of options to customise the behaviour of the group, see
`galenectl create-group -help` for a full list.  For example, in order to
create a group that allows unrestricted creation of tokens, say:

```sh
galenectl create-group -group city-watch -unrestricted-tokens
```

For more advanced configuration, `galenectl create-group` can be invoked
with the `-json` flag, in which case it takes a JSON template on standard
input.  The syntax of a JSON template is just like that of a group
definition file (see below), except that it must not contain the fields
`users` and `wildcard-user`.  For example, in order to create a redirect
(see the section "Group description reference" below):

```sh
echo '{"redirect": "https://galene.example.org:8443/group/city-watch/"}' | galenectl create-group -group amcw -json
```

Groups are modified using `galenectl update-group`:

```sh
galenectl update-group -group city-watch -unrestricted-tokens=false
```

If a JSON template is provided to `galenectl update-group`, then it is
merged with the existing group configuration.  Entries may be deleted
by setting them to `null` in the template:

```sh
echo '{"redirect": null}' | galenectl update-group -group amcw
```

A group is deleted using `galenectl delete-group`:

```sh
galenectl delete-group -group amcw
```

#### Creating, modifying, and deleting users

A user entry is created with the `galenectl create-user` command :

```sh
galenectl create-user -group city-watch -user vimes -permissions op
```

If the `-permissions` flag is not specified, it defaults to `present`,
meaning that the user can participate in the chat and present videos to
the group.  The other useful values are `message`, which allows a user
to participate in the chat only, and `observe`, which doesn't allow any
active participation.

A user is modified using `galenectl update-user`, and deleted using
`galenectl delete-user`.

In order to be useful, a user entry needs to be assigned a password.  This
is done with the `galenectl set-password` command:

```sh
galenectl set-password -group city-watch -user vimes
```

#### The fallback user

It is sometimes useful to allow multiple users to log-in using the same
password.  This is achieved by defining the *wildcard* user:

```sh
galenectl create-user -group city-watch -wildcard
galenectl set-password -group city-watch -wildcard
```

For open groups, where any user can login with any password, the wildcard
user's password is set to the password of type `wildcard`:

```sh
galenectl set-password -group city-watch -wildcard -type wildcard
```

See the section "Client authorisation" below for more information about
password types.

#### Automatic subgroups

It is sometimes necessary to create large numbers of identical groups.
For example, the author has been using Galene to supervise computer
science practicals, where up to 40 students are working in groups of two.

While it is possible to automate the creation of groups, by accessing
Galene's API, by scripting calls to galenectl, or by directly generating
files under `groups/`, Galene provides a facility known as *automatic
subgroups* that can be used to generate groups on demand.

Automatic subgroups are enabled by setting the `"auto-subgroups"`
field in the group description:

```sh
galenectl create-group unseen-university -auto-subgroups
```

Whenever a user attempts to access a subgroup of `unseen-university`, for
example `unseen-university/hex`, the group is created in memory and
persists until it is empty and its chat history has expired.  The main
group's operator can view the list of populated subgroups with the command
`/subgroups`.

#### Managing tokens

Tokens are normally managed using the `/invite`, `/reinvite` and `/expire`
commands in Galene's user interface, but they may also be managed using
the `galenectl` utility's `create-token`, `revoke-token`, `delete-token`
and `list-tokens` commands:

```sh
galenectl create-token -group city-watch
galenectl list-tokens -l -group city-watch
```

A token that is generated with the `-include-subgroups` flag applies to
the whole hierarchy rooted at the given group, including both ordinary
groups and automatically generated subgroups.

```sh
galenectl create-token -group city-watch -include-subgroups
```

Such a token can be attached to the root of the group hierarchy, and
therefore be valid for any group on the server:

```sh
galenectl create-token -group '' -include-subgroups
```

### Group description reference

The definition for the group called *groupname* is in the file
`groups/groupname.json`; it does not contain the group name, which makes
it easy to copy or link group definitions.  You may use subdirectories:
a file `groups/teaching/networking.json` defines a group called
*teaching/networking*.

Every group definition file contains a single JSON dictionary.  All fields
are optional.  The following fields are allowed:

 - `users`: is a dictionary that maps user names to user descriptions (see
   below);

 - `wildcard-user` is a user description that will be used for usernames
   with no matching entry in the `users` dictionary;

 - `authKeys`, `authServer` and `authPortal`: see *Authorisation* below;

 - `public`: if true, then the group is listed on the landing page;

 - `displayName`: a human-friendly version of the group name; this is
   displayed at the top of the group page;

 - `description`: a human-readable description of the group; this is
   displayed on the landing page for public groups;

 - `contact`: a human-readable contact for this group, such as an e-mail
   address, ignored by the server;

 - `comment`: a human-readable string, ignored by the server;

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

 - `codecs`: this is a list of codecs allowed in this group, see below for
   possible values.  The default is `["vp8", "opus"]`.

A user definition is a dictionary with entries `password` and
`permission`.  The value of the `password` field is either a plaintext
password, or a hashed password generated for example by the `galenectl
hash-password` command.  The value of the `permissions` field can either
be an array of individual permissions (not recommended), or one of the
following strings:

 - `op`, a group operator, with all rights except administering the group;
 - `present`, an ordinary user with the right to publish audio and video
   streams and send chat messages;
 - `message`, a user with the right to send chat messages;
 - `observe`, a user that receives media streams and chat messages, but
   is not allowed to send them;
 - `caption`, a user with the right to display captions (only);
 - `admin`, a user with the right to administer the group (only).

The value of the `codecs` field is an array of codecs allowed in the
group.  Supported video codecs include:

 - `"vp8"` (compatible with all supported browsers, full functionality);
 - `"vp9"` (better video quality, but incompatible with Safari; somewhat
   buggy in Firefox; full functionality);
 - `"av1"` (even better video quality, only supported by some browsers,
   limited functionality: no recording, no SVC);
 - `"h264"` (well supported by Apple devices, but incompatible with Debian
   Linux and with some older Android devices, SVC is not supported; might
   be covered by patents in some countries).

Supported audio codecs include `"opus"`, `"g722"`, `"pcmu"` and `"pcma"`.
Only Opus can be recorded to disk.  There is no good reason to use
anything except Opus.

## Client Authorisation

Galene implements three authorisation methods: a username/password
authorisation scheme, a scheme using stateful tokens, and a mechanism
based on cryptographic tokens.  The former two mechanism are intended to
be used in standalone installations, while the cryptographic mechanism is
designed to allow easy integration with an existing authorisation
infrastructure (such as LDAP, OAuth2, or even Unix passwords).

### Password authorisation

When password authorisation is used, authorised usernames and password are
defined directly in the group configuration file, in the `users` and
`wildcard-user` entries.  The `users` entry is a dictionary that maps user
names to user descriptions; the `wildcard-user` is a user description
that is used with usernames that don't appear in `users`.  These two
entries are usually managed by the `galenectl` utility.

Every user description is a dictionary with fields `password` and
`permissions`.  The `password` field may be a literal password string, or
a dictionary describing a hashed password or a wildcard.  The
`permissions` field should be one of `op`, `present`, `message` or
`observe`.  (An array of Galene's internal permissions is also allowed,
but this is not recommended, since internal permissions may vary from
version to version).

For example, the entry

```json
{
    "users": {"vimes": {"password": "sybil", "permissions": "op"}}
}
```

specifies that user "vimes" may login as operator with password "sybil", while

```json
{
    "wildcard-user": {"password": "1234", "permissions": "present"}
}
````

allows any username with password *1234*.  Finally,

```json
{
    "wildcard-user":
        {"password": {"type": "wildcard"}, "permissions": "present"}
}
```

allows any username with any password.

### Hashed passwords

For security reasons, passwords are usually hashed before being stored in
group descriptions (in fact, the `galenectl` utility does not even support
storing plaintext passwords).  A hashed password is represented as a JSON
dictionary with a field `type` and a number of type-specific fields.

A user entry with a hashed password looks like this:

```json
"users": {
    "vimes": {
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
```

Hashed passwords are normally generated transparently to the user by the
`galenectl set-password` command.  When editing group description files
manually, hashed passwords can be generated with the `galenectl hash-password`
utility.

### Stateful tokens

Stateful tokens are created by the `/invite` command in the Galene user
interface or by the `galenectl create-token` command; see the section
*Managing tokens* above.  They are stored in the file
`data/var/tokens.jsonl`, which, on most filesystems, can be safely backed
up without stopping the server.

### Cryptographic tokens

In many cases, it is useful to delegate authorisation decisions to a third
party, such as an LDAP or OAuth2 client.  Galene implements delegation of
authorisation decisions using cryptographic tokens generated by a third
party known as an *authorisaton server*.  Two authorisation servers are
available: an [LDAP client][2], and a [sample server written in Python][3].

When an authorisation server is used, the `"authKeys"` entry of the group
configuration file specifies one or more public keys in JWK format (with
the restriction that the "alg" key must be specified explicitly):

```json
{
    "authKeys": [{
        "kty": "oct",
        "alg": "HS256",
        "k": "MYz3IfCq4Yq-UmPdNqWEOdPl4C_m9imHHs9uveDUJGQ"
    }, {
        "kty": "EC",
        "alg": "ES256",
        "crv": "P-256",
        "x": "dElK9qBNyCpRXdvJsn4GdjrFzScSzpkz_I0JhKbYC88",
        "y": "pBhVb37haKvwEoleoW3qxnT4y5bK35_RTP7_RmFKR6Q"
    }]
}
```

If multiple keys are provided, then they will all be tried in turn, unless
the token includes the "kid" header field, in which case only the
specified key will be used.

The group file should also specify either an authorisation server or an
authorisation portal.  An authorisation server is specified using the
`"authServer"` key:

```json
{
    "authServer": "https://auth.example.org",
}
```

If an authorisation server is specified, then the client, after it prompts
for a password, will request a token from the authorisation server and
join the group using token authentication.  The password is never
communicated to the server.

Alternatively, the group file may specify an authorisation portal using
the `"authPortal"` key

If an authorisation portal is specified, then the default client will
redirect initial client connections to the authorisation portal.  The
authorisation portal is expected to authorise the client and then redirect
it to Galene with the `username` and `token` query parameters set.

[1]: <galene-install.md>
[2]: <https://github.com/jech/galene-imap/>
[3]: <https://github.com/jech/galene-sample-auth-server/>
