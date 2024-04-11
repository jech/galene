async function doit() {
    let groups = await listGroups();
    let s = document.getElementById('groups');
    for(let i = 0; i < groups.length; i++) {
        let group = groups[i];
        let e = document.createElement('button');
        e.textContent = "Edit";
        e.onclick = e => {
            e.preventDefault();
            editHandler(group);
        }
        let d = document.createElement('button');
        d.textContent = "Delete";
        d.onclick = e => {
            e.preventDefault();
            deleteHandler(group);
        }
        let p = document.createElement('p');
        p.textContent = group;
        p.appendChild(e);
        p.appendChild(d);
        s.appendChild(p);
    }
}

function editHandler(group) {
    document.location.href = `/manage-edit-group.html?group=${encodeURI(group)}`
}

async function deleteHandler(group) {
    let ok = confirm(`Do you want to delete group ${group}?`);
    if(ok) {
        try {
            await deleteGroup(group);
            location.reload();
        } catch(e) {
            displayError(e);
        }
    }
}

function displayError(message) {
    document.getElementById('errormessage').textContent = (message || '');
}

doit().catch(displayError);
