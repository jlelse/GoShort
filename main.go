package main

import (
	"database/sql"
	"fmt"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

var db *sql.DB

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	viper.SetDefault("dbPath", "data/goshort.db")
	viper.SetDefault("port", 8080)

	viper.SetConfigName("config")
	viper.AddConfigPath("./config")
	viper.AddConfigPath(".")
	_ = viper.ReadInConfig()

	if !viper.IsSet("dbPath") {
		log.Fatal("No database path (dbPath) is configured.")
	}
	if !viper.IsSet("password") {
		log.Fatal("No password (password) is configured.")
	}
	if !viper.IsSet("shortUrl") {
		log.Fatal("No short URL (shortUrl) is configured.")
	}
	if !viper.IsSet("defaultUrl") {
		log.Fatal("No default URL (defaultUrl) is configured.")
	}

	var err error
	db, err = sql.Open("sqlite3", viper.GetString("dbPath"))
	if err != nil {
		log.Fatal(err)
	}

	migrateDatabase()

	defer func() {
		_ = db.Close()
	}()

	r := mux.NewRouter()
	admin := r.NewRoute().Subrouter()
	admin.HandleFunc("/s", ShortenFormHandler).Methods(http.MethodGet)
	admin.HandleFunc("/s", ShortenHandler).Methods(http.MethodPost)
	admin.HandleFunc("/t", ShortenTextFormHandler).Methods(http.MethodGet)
	admin.HandleFunc("/t", ShortenTextHandler).Methods(http.MethodPost)
	admin.HandleFunc("/u", UpdateFormHandler).Methods(http.MethodGet)
	admin.HandleFunc("/ut", UpdateTextFormHandler).Methods(http.MethodGet)
	admin.HandleFunc("/u", UpdateHandler).Methods(http.MethodPost)
	admin.HandleFunc("/d", DeleteFormHandler).Methods(http.MethodGet)
	admin.HandleFunc("/d", DeleteHandler).Methods(http.MethodPost)
	admin.HandleFunc("/l", ListHandler).Methods(http.MethodGet)
	admin.Use(loginMiddleware)
	r.HandleFunc("/{slug}", ShortenedUrlHandler)
	r.HandleFunc("/", CatchAllHandler)

	addr := ":" + strconv.Itoa(viper.GetInt("port"))
	fmt.Println("Listening to " + addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

func loginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if !checkPassword(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ShortenFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateURLForm(w, "Shorten URL", "s", [][]string{{"url", r.FormValue("url")}, {"slug", r.FormValue("slug")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func UpdateFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateURLForm(w, "Update short link", "u", [][]string{{"slug", r.FormValue("slug")}, {"type", "url"}, {"new", r.FormValue("new")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func UpdateTextFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateTextForm(w, "Update text", "u", [][]string{{"slug", r.FormValue("slug")}, {"type", "text"}}, [][]string{{"new", r.FormValue("new")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func DeleteFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateURLForm(w, "Delete short link", "d", [][]string{{"slug", r.FormValue("slug")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func ShortenTextFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateTextForm(w, "Save text", "t", [][]string{{"slug", r.FormValue("slug")}}, [][]string{{"text", r.FormValue("text")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func generateURLForm(w http.ResponseWriter, title string, url string, fields [][]string) error {
	err := urlFormTemplate.Execute(w, &struct {
		Title  string
		Url    string
		Fields [][]string
	}{
		Title:  title,
		Url:    url,
		Fields: fields,
	})
	if err != nil {
		return err
	}
	return nil
}

func generateTextForm(w http.ResponseWriter, title string, url string, fields [][]string, textAreas [][]string) error {
	err := textFormTemplate.Execute(w, &struct {
		Title     string
		Url       string
		Fields    [][]string
		TextAreas [][]string
	}{
		Title:     title,
		Url:       url,
		Fields:    fields,
		TextAreas: textAreas,
	})
	if err != nil {
		return err
	}
	return nil
}

func ShortenHandler(w http.ResponseWriter, r *http.Request) {
	writeShortenedUrl := func(w http.ResponseWriter, slug string) {
		_, _ = w.Write([]byte(viper.GetString("shortUrl") + "/" + slug))
	}

	requestUrl := r.FormValue("url")
	if requestUrl == "" {
		http.Error(w, "url parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.FormValue("slug")
	manualSlug := false
	if slug == "" {
		_ = db.QueryRow("SELECT slug FROM redirect WHERE url = ?", requestUrl).Scan(&slug)
	} else {
		manualSlug = true
	}

	if slug != "" {
		if _, e := slugExists(slug); e {
			if manualSlug {
				http.Error(w, "slug already in use", http.StatusBadRequest)
				return
			}
			writeShortenedUrl(w, slug)
			return
		}
	} else {
		var exists = true
		for exists == true {
			slug = generateSlug()
			var err error
			err, exists = slugExists(slug)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	_, err := db.Exec("INSERT INTO redirect (slug, url) VALUES (?, ?)", slug, requestUrl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeShortenedUrl(w, slug)
}

func ShortenTextHandler(w http.ResponseWriter, r *http.Request) {
	writeShortenedUrl := func(w http.ResponseWriter, slug string) {
		_, _ = w.Write([]byte(viper.GetString("shortUrl") + "/" + slug))
	}

	requestText := r.FormValue("text")
	if requestText == "" {
		http.Error(w, "text parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.FormValue("slug")
	manualSlug := false
	if slug == "" {
		_ = db.QueryRow("SELECT slug FROM redirect WHERE url = ? and type = 'text'", requestText).Scan(&slug)
	} else {
		manualSlug = true
	}

	if slug != "" {
		if _, e := slugExists(slug); e {
			if manualSlug {
				http.Error(w, "slug already in use", http.StatusBadRequest)
				return
			}
			writeShortenedUrl(w, slug)
			return
		}
	} else {
		var exists = true
		for exists == true {
			slug = generateSlug()
			var err error
			err, exists = slugExists(slug)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	_, err := db.Exec("INSERT INTO redirect (slug, url, type) VALUES (?, ?, 'text')", slug, requestText)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeShortenedUrl(w, slug)
}

func UpdateHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.FormValue("slug")
	if slug == "" {
		http.Error(w, "Specify the slug to update", http.StatusBadRequest)
		return
	}

	newUrl := r.FormValue("new")
	if newUrl == "" {
		http.Error(w, "Specify the new URL", http.StatusBadRequest)
		return
	}

	typeString := r.FormValue("type")
	if typeString == "" {
		typeString = "url"
	}

	if err, e := slugExists(slug); !e || err != nil {
		http.Error(w, "Slug not found", http.StatusNotFound)
		return
	}

	_, err := db.Exec("UPDATE redirect SET url = ?, type = ? WHERE slug = ?", newUrl, typeString, slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Slug updated"))
}

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.FormValue("slug")
	if slug == "" {
		http.Error(w, "Specify the slug to delete", http.StatusBadRequest)
		return
	}

	if err, e := slugExists(slug); !e || err != nil {
		http.Error(w, "Slug not found", http.StatusNotFound)
		return
	}

	_, err := db.Exec("DELETE FROM redirect WHERE slug = ?", slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Slug deleted"))
}

func ListHandler(w http.ResponseWriter, r *http.Request) {
	type row struct {
		Slug string
		Url  string
		Hits int
	}
	var list []row
	rows, err := db.Query("SELECT slug, url, hits FROM redirect")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var r row
		err = rows.Scan(&r.Slug, &r.Url, &r.Hits)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, r)
	}
	err = listTemplate.Execute(w, &list)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func checkPassword(w http.ResponseWriter, r *http.Request) bool {
	if r.FormValue("password") == viper.GetString("password") {
		return true
	}
	_, pass, ok := r.BasicAuth()
	if !(ok && pass == viper.GetString("password")) {
		w.Header().Set("WWW-Authenticate", `Basic realm="Please enter a password!"`)
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return false
	}
	return true
}

func generateSlug() string {
	var chars = []rune("0123456789abcdefghijklmnopqrstuvwxyz")
	s := make([]rune, 6)
	for i := range s {
		s[i] = chars[rand.Intn(len(chars))]
	}
	return string(s)
}

func slugExists(slug string) (e error, exists bool) {
	err := db.QueryRow("SELECT EXISTS(SELECT * FROM redirect WHERE slug = ?)", slug).Scan(&exists)
	if err != nil {
		return err, false
	}

	return nil, exists
}

func ShortenedUrlHandler(w http.ResponseWriter, r *http.Request) {
	slug, ok := mux.Vars(r)["slug"]
	if !ok {
		CatchAllHandler(w, r)
		return
	}

	var redirectUrl string
	var typeString string
	err := db.QueryRow("SELECT url, type FROM redirect WHERE slug = ?", slug).Scan(&redirectUrl, &typeString)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	go func() {
		_, _ = db.Exec("UPDATE redirect SET hits = hits + 1 WHERE slug = ?", slug)
	}()

	if typeString == "text" {
		_, _ = w.Write([]byte(redirectUrl))
	} else {
		http.Redirect(w, r, redirectUrl, http.StatusTemporaryRedirect)
	}
}

func CatchAllHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, viper.GetString("defaultUrl"), http.StatusTemporaryRedirect)
}
