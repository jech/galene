// Copyright (c) 2020 by Juliusz Chroboczek.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.  IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

'use strict';

document.getElementById('passwordform').onsubmit = async function(e) {
    e.preventDefault();

    let parms = new URLSearchParams(window.location.search);
    let group = parms.get('group');
    if(!group) {
        displayError("Couldn't determine group");
        return;
    }
    let user = parms.get('username');
    if(!user) {
        displayError("Couldn't determine username");
        return;
    }

    let old1 = document.getElementById('old1').value;
    let old2 = document.getElementById('old2').value;
    if(old1 !== old2) {
        displayError("Passwords don't match.");
        return;
    }

    try {
        await doit(group, user, old1, document.getElementById('new').value);
        document.getElementById('old1').value = '';
        document.getElementById('old2').value = '';
        document.getElementById('new').value = '';
        displayError(null);
        document.getElementById('message').textContent =
            'Password successfully changed.';
    } catch(e) {
        displayError(e.toString());
    }
}

async function doit(group, user, old, pw) {
    let creds = btoa(user + ":" + old);
    let r = await fetch(`/galene-api/0/.groups/${group}/.users/${user}/.password`,
                        {
                            method: 'POST',
                            body: pw,
                            credentials: 'omit',
                            headers: {
                                'Authorization': `Basic ${creds}`
                            }
                        });
    if(!r.ok) {
        if(r.status === 401)
            throw new Error('Permission denied');
        else
            throw new Error(`The server said: ${r.status} ${r.statusText}`);
        return;
    }
}

function displayError(message) {
    document.getElementById('errormessage').textContent = (message || '');
}
