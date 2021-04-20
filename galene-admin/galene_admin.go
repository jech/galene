package main

import (
	"net/http"
	"log"
	"github.com/jech/galene/galene-admin/serverAdmin"
)


func main() {
	serverAdmin.Handle()

	log.Fatal(http.ListenAndServe(":8444", nil))

}
