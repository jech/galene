package serverAdmin

import(
	"net/http"
	"fmt"
	"io/ioutil"
	"html/template"
	"strings"
	"os"
	"path"
	"time"
	"encoding/json"
	"path/filepath"
	"strconv"
	"crypto/rand"
	"io"
	"encoding/base64"
)

var MainStaticRoot string = "../static"

var StaticRoot string = "./static"

var dirGroups string = "../groups/"

var globalUser map[string]*TimeSession = make(map[string]*TimeSession)

var cookie_token string = "session_token"

type TimeSession struct {
	LastAccess	time.Time
	Expiration	time.Duration
}

type User struct {
	Username	string
	Password	string
}

type Config struct {
	Admin	[]User
}

type Form struct {
    Name	string
	Error	string
}

type Description struct {
	FileName		string	`json:"-"`
	Op				[]User	`json:"op"`
	Presenter		[]User	`json:"presenter"`
	Other			[]User	`json:"other"`
	Public			bool	`json:"public,omitempty"`
	Description		string	`json:"description,omitempty"`
	Contact			string	`json:"contact,omitempty"`
	Comment			string	`json:"comment,omitempty"`
	MaxClients		int		`json:"max-clients,omitempty"`
	MaxHistoryAge	int		`json:"max-history-age,omitempty"`
	AllowRecording	bool	`json:"allow-recording,omitempty"`
	AllowAnonymous	bool	`json:"allow-anonymous,omitempty"`
	AllowSubgroups	bool	`json:"allow-subgroups,omitempty"`
	Autolock		bool	`json:"autolock,omitempty"`
	Redirect		string	`json:"redirect,omitempty"`
}

type FilesJson struct {
	Files	[]Description
}

type FilesJsonForm struct {
	Files	FilesJson
	Form	Form
}

func Handle() {
	http.HandleFunc("/", indexAdmin)
	http.HandleFunc("/groups", groupAdmin)
	http.HandleFunc("/modify-group/", modifyGroupAdmin)
}

func indexAdmin(w http.ResponseWriter, r *http.Request) {
	fileName := r.URL.Path

	if fileName == "/" {
		fileName = "/index.html"
	}

	if !strings.HasSuffix(fileName, ".html") {
		sendNotHTMLPage(w, r, fileName)
		return;
	}

	var f = Form{Name: "",	Error: ""}

	if r.Method == "POST" {
		// Call ParseForm() to parse the raw query and update r.PostForm and r.Form.
		err := r.ParseForm();
		if err == nil  && r.FormValue("submit") == "Connect"{

			usernameAdmin := r.FormValue("usernameAdmin")
			passwordAdmin := r.FormValue("passwordAdmin")

			f.Name = usernameAdmin

			config := getAdminUsers()

			for i := range config.Admin {
				if usernameAdmin == config.Admin[i].Username && passwordAdmin == config.Admin[i].Password {
					tab := make([]byte, 18)
					io.ReadFull(rand.Reader, tab)
					sessionToken := base64.URLEncoding.EncodeToString(tab)

					http.SetCookie(w, &http.Cookie{
						Name:    cookie_token,
						Value:   sessionToken,
						Expires: time.Now().Add(120 * time.Second),
					})
					globalUser[sessionToken] = &TimeSession{LastAccess : time.Now(), Expiration: 120 * time.Second}

					http.Redirect(w, r, "/groups", http.StatusPermanentRedirect)
					return
				}
			}
			f.Error = "Admin not found, try to check your username and your password"
		}
	}


	t, _ := template.ParseFiles(StaticRoot + fileName)
	t.Execute(w, f)

}

func groupAdmin(w http.ResponseWriter, r *http.Request) {
	if !testCookie(w, r) {
		return
	}

	filename := r.URL.Path
	if filename == "/groups" {
		filename = StaticRoot + "/groups.html"
	}
	if !strings.HasSuffix(filename, ".html") {
		sendNotHTMLPage(w, r, filename)
		return;
	}

	var f = Form{Name: "",	Error: ""}
	var fj, _ = getJson()

	if r.Method == "POST" {
		err := r.ParseForm();

		if err == nil && r.FormValue("submit") == "Create" {
			nameGroup := r.FormValue("nameGroup")
			alreadyExists := false

			for i := 0; i < len(fj.Files); i++ {
				if nameGroup == fj.Files[0].FileName {
					alreadyExists = true
				}
			}
			if strings.ContainsAny(nameGroup, "/ .<>?,;:!") {
				f.Error = "Illegal character"
			}

			if len(nameGroup) == 0 {
				f.Error = "The name can't be empty"
			}

			if alreadyExists {
				f.Error = "This groupname alreay exists"
			}

			if f.Error == "" {
				emptyFile, err := os.Create(dirGroups + nameGroup + ".json")
				if err != nil {
					fmt.Printf("Error creation file\n")
				} else {
					_, err := emptyFile.WriteString("{\n\t\"op\": [{}],\n\t\"presenter\": [{}],\n\t\"other\": [{}]\n}")
					if err != nil {
						fmt.Println(err)
						emptyFile.Close()
						return
					}
					err = emptyFile.Close()
					if err != nil {
						fmt.Println(err)
						return
					}
					http.Redirect(w, r, "modify-group/" + nameGroup, http.StatusPermanentRedirect)
					return;
				}
			}
			f.Name = nameGroup
		}
	}


	var fileForm = FilesJsonForm{Files: *fj, Form: f}
	t, _ := template.ParseFiles(filename)
	t.Execute(w, fileForm)
}

func modifyGroupAdmin(w http.ResponseWriter, r *http.Request) {
	if !testCookie(w, r) {
		return
	}


	filename := r.URL.Path
	if strings.HasPrefix(filename, "/modify-group/") {
		filename = StaticRoot + "/modify-group.html"
	}

	if !strings.HasSuffix(filename, ".html") {
		sendNotHTMLPage(w, r, filename)
		return;
	}

	name := r.URL.Path[len("/modify-group/"):]
	if name == "" {
		send404(w, r)
		return
	}

	fileJson := dirGroups + name + ".json"
	data, err := ioutil.ReadFile(fileJson)
	if err != nil {
		fmt.Println(err)
		send404(w, r)
		return
	}
	var fj Description
	fj.FileName = name

	json.Unmarshal([]byte(data), &fj)

	if r.Method == "POST" {
		err := r.ParseForm();
		if err == nil {

			fj.Op = make([]User, 0)
			fj.Presenter = make([]User, 1)
			fj.Other = make([]User, 1)

			if r.FormValue("publicGroup") == "on" {
				fj.Public = true;
			}

			fj.Description = r.FormValue("descriptionGroup")
			fj.Contact = r.FormValue("contactGroup")
			fj.Comment = r.FormValue("commentGroup")

			var convertInt int

			convertInt, err := strconv.Atoi(r.FormValue("maxClientGroup"))
			if err == nil {
				fj.MaxClients = convertInt
			}
			convertInt, err = strconv.Atoi(r.FormValue("maxAgeGroup"))
			if err == nil {
				fj.MaxHistoryAge = convertInt
			}

			if r.FormValue("allowRecordGroup") == "on" {
				fj.AllowRecording = true;
			}
			if r.FormValue("allowAnonymGroup") == "on" {
				fj.AllowAnonymous = true;
			}
			if r.FormValue("allowSubGroup") == "on" {
				fj.AllowSubgroups = true;
			}
			if r.FormValue("autolockGroup") == "on" {
				fj.Autolock = true;
			}

			fj.Redirect = r.FormValue("redirectGroup")

			var u User
			for key, values := range r.Form {
				if len(values) == 2 {
					if (values[0] != "" || values[1] != ""){
						u.Username = values[0]
						u.Password = values[1]

						if strings.HasPrefix(key, "opGroup") {
							fj.Op = append(fj.Op, u)
						}
						if strings.HasPrefix(key, "presenter") {
							fj.Presenter = appendUser(fj.Presenter, u)
						}
						if strings.HasPrefix(key, "other") {
							fj.Other = appendUser(fj.Other, u)
						}
					}
				}
			}
			data, _ := json.Marshal(fj)

			file, err := os.OpenFile(fileJson, os.O_RDWR|os.O_TRUNC, 0)
			if err != nil {
				fmt.Println(err)
				return;
			}
			dataPropre := makeJSONReadable(string(data))

			_, err = file.WriteString(dataPropre)
			if err != nil {
				fmt.Println(err)
				file.Close()
				return
			}
		}
	}


	t, _ := template.ParseFiles(filename)
	t.Execute(w, fj)
}

func getJson() (*FilesJson, error) {
	var filesName []string;
	err := filepath.Walk(dirGroups, func(path string, fi os.FileInfo, err error) error {
		if fi.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".json") {
			filesName = append(filesName, path)
		}
		return nil
	});
	if err != nil {
		var fj = FilesJson{Files : make([]Description, 0)}
		return &fj,err
	}
	var fj = FilesJson{Files : make([]Description, len(filesName))}

	for i := 0; i < len(filesName); i++ {
		data, err := ioutil.ReadFile(filesName[i])
		if err != nil {
			fmt.Println("File reading error", err)
		} else {
			json.Unmarshal([]byte(data), &(fj.Files[i]))
			fj.Files[i].FileName = strings.TrimPrefix(strings.TrimSuffix(filesName[i], ".json"), dirGroups)
		}
	}
	return &fj, nil
}

func getAdminUsers() (*Config) {
	var config Config
	data, err := ioutil.ReadFile("admin.json")
    if err != nil {
        fmt.Println("File reading error", err)
        return &config
    }
	json.Unmarshal([]byte(data), &config)
	return &config
}

func sendNotHTMLPage(w http.ResponseWriter, r *http.Request, fileName string)  {
	var filePath string
	//If not HTML, try galene css/js, then galene-admin css/js
	filePath = MainStaticRoot + fileName
	_, err := os.Open(filePath)
	if err != nil {
		filePath = StaticRoot + fileName
	}

	file, err := os.Open(filePath)
	if err != nil {
		send404(w, r)
		return;
	}
	defer file.Close()
	_, filename := path.Split(filePath)
	http.ServeContent(w, r, filename, time.Time{}, file)

}

func send404(w http.ResponseWriter, r *http.Request) {
	filePath := MainStaticRoot + "/404.html"
	file, err := os.Open(filePath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "<html><body style='font-size:100px'>four-oh-four</body></html>")
		return
	}
	defer file.Close()
	_, filename := path.Split(filePath)
	http.ServeContent(w, r, filename, time.Time{}, file)
}
func send401(w http.ResponseWriter, r *http.Request) {
	filePath := StaticRoot + "/401.html"
	file, err := os.Open(filePath)
	if err != nil {
		send404(w, r)
		return
	}
	defer file.Close()
	_, filename := path.Split(filePath)
	http.ServeContent(w, r, filename, time.Time{}, file)
}
func appendUser(users []User, u User) ([]User) {
	for i := range users {
		if users[i].Username == "" && users[i].Password == "" {
			users[i] = u
			return users
		}
	}
	return append(users, u)
}

func makeJSONReadable(data string) (string) {
	data = strings.Replace(data, "}],", "}],\n\t", 2)
	data = strings.Replace(data, "{", "{\n\t", 1)
	data = strings.ReplaceAll(data, ":", ": ")

	lastTab := strings.LastIndex(data, "]")

	data = strings.ReplaceAll(data[0:lastTab], ",", ", ") + data[lastTab:]
	data = data[0:lastTab] + strings.ReplaceAll(data[lastTab:], ",", ",\n\t")

	data = data[0:len(data) - 1] + "\n}"
	return data
}

func checkUsers(sessionToken string) (bool) {
	tmp := make([]string, len(globalUser))
	size := 0
	sameCookie := false
	var ts *TimeSession

	for s := range globalUser {
		ts = globalUser[s]

		if time.Now().Sub(ts.LastAccess) > ts.Expiration {
			tmp[size] = s
			size++
		} else {
			if sessionToken == s {
				sameCookie = true
				ts.LastAccess = time.Now()
			}
		}
	}

	for i := 0; i < size; i++ {
		delete(globalUser, tmp[i])
	}

	return sameCookie
}

func testCookie(w http.ResponseWriter, r *http.Request) (bool) {
	c, err := r.Cookie(cookie_token)
	if err != nil {
		// If the cookie is not set, return an unauthorized status
		send401(w, r)
		return false
	}
	sessionToken := c.Value
	if !checkUsers(sessionToken) {
		send401(w, r)
		return false
	}
	return true
}
