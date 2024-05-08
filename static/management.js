// Copyright (c) 2024 by Juliusz Chroboczek.

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

/**
 * httpError returns an error that encapsulates the status of the response r.
 *
 * @param {Response} r
 * @returns {Error}
 */
function httpError(r) {
    let s = r.statusText;
    if(s === '') {
        switch(r.status) {
            case 401: s = 'Unauthorised'; break;
            case 403: s = 'Forbidden'; break;
            case 404: s = 'Not Found'; break;
        }
    }

    return new Error(`The server said: ${r.status} ${s}`);
}

/**
 * listObjects fetches a list of strings from the given URL.
 *
 * @param {string} url
 * @returns {Promise<Array<string>>}
 */
async function listObjects(url) {
    let r = await fetch(url);
    if(!r.ok)
        throw httpError(r);
    let data = await r.json();
    if(!(data instanceof Array))
        throw new Error("Server didn't return array");
    return data;
 }

/**
 * createObject makes a PUT request to url with JSON data.
 * It fails if the object already exists.
 *
 * @param {string} url
 * @param {Object} [values]
 */
async function createObject(url, values) {
    if(!values)
        values = {};
    let r = await fetch(url, {
        method: 'PUT',
        body: JSON.stringify(values),
        headers: {
            'If-None-Match': '*',
            'Content-Type:': 'application/json',
        }
    });
    if(!r.ok)
        throw httpError(r);
}

/**
 * getObject fetches the JSON object at a given URL.
 * If an ETag is provided, it fails if the ETag didn't match.
 *
 * @param {string} url
 * @param {string} [etag]
 * @returns {Promise<Object>}
 */
async function getObject(url, etag) {
    let options = {};
    if(etag) {
        options.headers = {
            'If-Match': etag
        }
    }
    let r = await fetch(url, options);
    if(!r.ok)
        throw httpError(r);
    let newetag = r.headers.get("ETag");
    if(!newetag)
        throw new Error("The server didn't return an ETag");
    if(etag && newetag !== etag)
        throw new Error("The server returned a mismatched ETag");
    let data = await r.json();
    return {etag: newetag, data: data}
}

/**
 * deleteObject makes a DELETE request to the given URL.
 * If an ETag is provided, it fails if the ETag didn't match.
 *
 * @param {string} url
 * @param {string} [etag]
 */
async function deleteObject(url, etag) {
    /** @type {Object<string, string>} */
    let headers = {};
    if(etag)
        headers['If-Match'] = etag;
    let r = await fetch(url, {
        method: 'DELETE',
        headers: headers,
    });
    if(!r.ok)
        throw httpError(r);
}

/**
 * updateObject makes a read-modify-write cycle on the given URL.  Any
 * fields that are non-null in values are added or modified, any fields
 * that are null are deleted, any fields that are absent are left unchanged.
 *
 * @param {string} url
 * @param {Object} values
 * @param {string} [etag]
 */
async function updateObject(url, values, etag) {
    let old = await getObject(url, etag);
    let data = old.data;
    for(let k in values) {
        if(values[k])
            data[k] = values[k];
        else
            delete(data[k])
    }
    let r = await fetch(url, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'If-Match': old.etag,
        }
    })
    if(!r.ok)
        throw httpError(r);
}

/**
 * listGroups returns the list of groups.
 *
 * @returns {Promise<Array<string>>}
 */
async function listGroups() {
    return await listObjects('/galene-api/v0/.groups/');
}

/**
 * getGroup returns the sanitised description of the given group.
 *
 * @param {string} group
 * @param {string} [etag]
 * @returns {Promise<Object>}
 */
async function getGroup(group, etag) {
    return await getObject(`/galene-api/v0/.groups/${group}`, etag);
}

/**
 * createGroup creates a group.  It fails if the group already exists.
 *
 * @param {string} group
 * @param {Object} [values]
 */
async function createGroup(group, values) {
    return await createObject(`/galene-api/v0/.groups/${group}`, values);
}

/**
 * deleteGroup deletes a group.
 *
 * @param {string} group
 * @param {string} [etag]
 */
async function deleteGroup(group, etag) {
    return await deleteObject(`/galene-api/v0/.groups/${group}`, etag);
}

/**
 * updateGroup modifies a group definition.
 * Any fields present in values are overriden, any fields absent in values
 * are left unchanged.
 *
 * @param {string} group
 * @param {Object} values
 * @param {string} [etag]
 */
async function updateGroup(group, values, etag) {
    return await updateObject(`/galene-api/v0/.groups/${group}`, values);
}

/**
 * listUsers lists the users in a given group.
 *
 * @param {string} group
 * @returns {Promise<Array<string>>}
 */
async function listUsers(group) {
    return await listObjects(`/galene-api/v0/.groups/${group}/.users/`);
}

/**
 * userURL returns the URL for a user entry
 *
 * @param {string} group
 * @param {string} user
 * @param {boolean} wildcard
 */
function userURL(group, user, wildcard) {
    if(wildcard)
        return `/galene-api/v0/.groups/${group}/.wildcard-user`;
    else if(user === "")
        return `/galene-api/v0/.groups/${group}/.empty-user`;
    else
        return `/galene-api/v0/.groups/${group}/.users/${user}`
}

/**
 * getUser returns a given user entry.
 *
 * @param {string} group
 * @param {string} user
 * @param {boolean} wildcard
 * @param {string} [etag]
 * @returns {Promise<Object>}
 */
async function getUser(group, user, wildcard, etag) {
    return await getObject(userURL(group, user, wildcard), etag);
}

/**
 * createUser creates a new user entry.  It fails if the user already
 * exists.
 *
 * @param {string} group
 * @param {string} user
 * @param {boolean} wildcard
 * @param {Object} values
 */
async function createUser(group, user, wildcard, values) {
    return await createObject(userURL(group, user, wildcard), values);
}

/**
 * deleteUser deletes a user.
 *
 * @param {string} group
 * @param {string} user
 * @param {boolean} wildcard
 * @param {string} [etag]
 */
async function deleteUser(group, user, wildcard, etag) {
    return await deleteObject(userURL(group, user, wildcard), etag);
}

/**
 * updateUser modifies a given user entry.
 *
 * @param {string} group
 * @param {string} user
 * @param {Object} values
 * @param {boolean} wildcard
 * @param {string} [etag]
 */
async function updateUser(group, user, wildcard, values, etag) {
    return await updateObject(userURL(group, user, wildcard),  values, etag);
}

/**
 * setPassword sets a user's password.
 * If oldpassword is provided, then it is used for authentication instead
 * of the browser's normal mechanism.
 *
 * @param {string} group
 * @param {string} user
 * @param {boolean} wildcard
 * @param {string} password
 * @param {string} [oldpassword]
 */
async function setPassword(group, user, wildcard, password, oldpassword) {
    let options = {
        method: 'POST',
        headers: {
            'Content-Type': 'text/plain'
        },
        body: password,
    }
    if(oldpassword) {
        options.credentials = 'omit';
        options.headers['Authorization'] =
            `Basic ${btoa(user + ':' + oldpassword)}`
    }

    let r = await fetch(userURL(group, user, wildcard) + '/.password', options);
    if(!r.ok)
        throw httpError(r);
}

/**
 * listTokens lists the tokens for a given group.
 *
 * @param {string} group
 * @returns {Promise<Array<string>>}
 */
async function listTokens(group) {
    return await listObjects(`/galene-api/v0/.groups/${group}/.tokens/`);
}

/**
 * getToken returns a given token.
 *
 * @param {string} group
 * @param {string} token
 * @param {string} [etag]
 * @returns {Promise<Object>}
 */
async function getToken(group, token, etag) {
    return await getObject(`/galene-api/v0/.groups/${group}/.tokens/${token}`,
                           etag);
}

/**
 * createToken creates a new token and returns its name
 *
 * @param {string} group
 * @param {Object} template
 * @returns {Promise<string>}
 */
async function createToken(group, template) {
    let options = {
        method: 'POST',
        headers: {
            'Content-Type': 'text/json'
        },
        body: template,
    }

    let r = await fetch(
        `/galene-api/v0/.groups/${group}/.tokens/`,
        options);
    if(!r.ok)
        throw httpError(r);
    let t = r.headers.get('Location');
    if(!t)
        throw new Error("Server didn't return location header");
    return t;
}

/**
 * updateToken modifies a token.
 *
 * @param {string} group
 * @param {Object} token
 */
async function updateToken(group, token, etag) {
    if(!token.token)
        throw new Error("Unnamed token");
    return await updateObject(
        `/galene-api/v0/.groups/${group}/.tokens/${token.token}`,
        token, etag);
}
