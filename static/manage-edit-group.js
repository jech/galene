async function doit(group) {
    let de = await getGroup(group);
    let d = document.getElementById('description');
    for(let k in de.data) {
        let p = document.createElement('p');
        p.textContent = `${k}: ${de.data[k]}`;
        d.appendChild(p);
    }

    let users = await listUsers(group);
    let u = document.getElementById('users');
    for(let i = 0; i < users.length; i++) {
        let username = users[i];
        let ut = await getUser(group, username, false);
        let p = document.createElement('p');
        p.textContent = `${username} ${ut.data.permissions}`
        u.appendChild(p);
    }

    let tokens = await listTokens(group);
    let t = document.getElementById('tokens');
    for(let i = 0; i < tokens.length; i++) {
        let token = tokens[i];
        let tt = await getToken(group, token);
        let p = document.createElement('p');
        p.textContent = `${token} ${tt.expires || '(no expiration)'}`
        t.appendChild(p);
    }
}


function displayError(message) {
    document.getElementById('errormessage').textContent = (message || '');
}

let parms = new URLSearchParams(window.location.search);
if(!parms.has('group')) {
    displayError('Unknown group');
} else {
    doit(parms.get('group')).catch(displayError);
}
