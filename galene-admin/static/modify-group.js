document.getElementById("modifyGroupForm").onkeypress = function(e) {
	if(e.key == "Enter") {
		e.preventDefault();
	}
}


function addUser(nameMaj, nameMin) {
	let newElement = document.createElement("div");
	let button = document.getElementById("add" + nameMaj);
	let div = document.getElementById("first" + nameMaj + "Add");
	let sizeOfOpSupp = document.getElementsByClassName(nameMin + "Supp").length;
	console.log(sizeOfOpSupp);
	newElement.innerHTML += div.innerHTML;
	newElement.innerHTML = (newElement.innerHTML.replace("Additional " + nameMaj, "Additional " + nameMaj + " " + sizeOfOpSupp)).replace(/GroupSupp/g, "GroupSupp" + sizeOfOpSupp)

	document.getElementById("modifyGroupForm").insertBefore(newElement, button);

}
document.getElementById('addOp').onclick = function(e) {
	console.log(e);
    e.preventDefault();
	addUser("Op", "op");
};

document.getElementById('addPresenter').onclick = function(e) {
    e.preventDefault();
	addUser("Presenter", "presenter");
};
document.getElementById('addOther').onclick = function(e) {
    e.preventDefault();
	addUser("Other", "other");
};
