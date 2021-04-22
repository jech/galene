package main

import (
	"net/http"
	"log"
	"flag"
	"os"

	"github.com/jech/galene/galene-admin/serverAdmin"
)


func main() {
	var galene_path string = "../"

	if len(os.Args) > 1 {
		galene_path = os.Args[1]
	}

	flag.StringVar(&serverAdmin.StaticRoot, "static", "./static",
		 "web server root")
	flag.StringVar(&serverAdmin.MainStaticRoot, "Galene static",
		 galene_path + "static", "path to Galène web server root")
	flag.StringVar(&serverAdmin.DirGroups, "groups directory",
		 galene_path + "groups/", "groups directory")
	flag.StringVar(&serverAdmin.Cookie_token, "cookie",
		 "session_token", "token for simulate session")
	flag.StringVar(&serverAdmin.AdminFile, "admin file",
		 "admin.json", "file with admin's usernames/passwords")
	flag.Parse()


	serverAdmin.Handle()

	log.Fatal(http.ListenAndServe(":8444", nil))

}
