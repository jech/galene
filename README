# Installation

See the file INSTALL in this directory for installation instructions.


# Usage

## Locations

There is a landing page at the root of the server.  It contains a form
for typing the name of a group, and a clickable list of public groups.

Groups are available under `/group/groupname`.  You may share this URL
with others, there is no need to go through the landing page.

Recordings can be accessed under `/recordings/groupname`.  This is only
available to the administrator of the group.

Some statistics are available under `/stats.json`, with a human-readable
version at `/stats.html`.  This is only available to the server administrator.


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


# The global configuration file

The server may be configured in the JSON file `data/config.json`.  This
file may look as follows:

    {
        "canonicalHost": "galene.example.org",
        "admin":[{"username":"root","password":"secret"}]
    }

The fields are as follows:

- `canonicalHost`: the canonical name of the host running the server;
- `admin` defines the users allowed to look at the `/stats.html` file; it
  has the same syntax as user definitions in groups (see below).


# Group definitions

Groups are defined by files in the `./groups` directory (this may be
configured by the `-groups` command-line option, try `./galene -help`).
The definition for the group called *groupname* is in the file
`groups/groupname.json`; it does not contain the group name, which makes
it easy to copy or link group definitions.  You may use subdirectories:
a file `groups/teaching/networking.json` defines a group called
*teaching/networking*.

A typical group definition file looks like this:

    {
        "op":[{"username":"jch","password":"1234"}],
        "presenter":[{}]
        "allow-recording": true,
        "allow-subgroups": true
    }

This defines a group with the operator (administrator) username *jch* and
password *1234*, empty username and password for presenters (ordinary
users with the right to enable their camera and microphone).  The
`allow-recording` entry says that the operator is allowed to record videos
to disk, and the `allow-subgroups` entry says that subgroups will be
created automatically.

More precisely, every group definition file contains a single JSON
directory (a list of entries between `{' and `}').  All fields are
optional, but unless you specify at least one user definition (`op`,
`presenter`, or `other`), nobody will be able to join the group.  The
following fields are allowed:

 - `op`, `presenter`, `other`: each of these is an array of user
   definitions (see below) and specifies the users allowed to connect
   respectively with operator privileges, with presenter privileges, and
   as passive listeners;
 - `public`: if true, then the group is visible on the landing page;
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
 - `"vp9"` (better video quality, but incompatible with Safari);
 - `"av1"` (even better video quality, only supported by some browsers,
   recording is not supported, SVC is not supported);
 - `"h264"` (incompatible with Debian and with some Android devices, SVC
   is not supported).

Supported audio codecs include `"opus"`, `"g722"`, `"pcmu"` and `"pcma"`.
Only Opus can be recorded to disk.  There is no good reason to use
anything except Opus.
   
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


# Further information

Galène's web page is at <https://galene.org>.

Answers to common questions and issues are at <https://galene.org/faq.html>.


-- Juliusz Chroboczek <https://www.irif.fr/~jch/>
