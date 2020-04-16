package main

import (
	"database/sql"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rubenv/sql-migrate"
	"github.com/spf13/viper"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"time"
)

var db *sql.DB

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	viper.SetDefault("dbPath", "data/goshort.db")

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
	admin.HandleFunc("/u", UpdateFormHandler).Methods(http.MethodGet)
	admin.HandleFunc("/u", UpdateHandler).Methods(http.MethodPost)
	admin.HandleFunc("/d", DeleteFormHandler).Methods(http.MethodGet)
	admin.HandleFunc("/d", DeleteHandler).Methods(http.MethodPost)
	admin.HandleFunc("/l", ListHandler).Methods(http.MethodGet)
	admin.Use(loginMiddleware)
	r.HandleFunc("/{slug}", ShortenedUrlHandler)
	r.HandleFunc("/", CatchAllHandler)

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe(":8080", nil))
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

func migrateDatabase() {
	migrations := &migrate.MemoryMigrationSource{
		Migrations: []*migrate.Migration{
			{
				Id:   "001",
				Up:   []string{"create table redirect(slug text not null primary key,url text not null,hits integer default 0 not null);insert into redirect (slug, url) values ('source', 'https://git.jlel.se/jlelse/GoShort');"},
				Down: []string{"drop table redirect;"},
			},
		},
	}
	_, err := migrate.Exec(db, "sqlite3", migrations, migrate.Up)
	if err != nil {
		log.Fatal(err)
	}
}

func ShortenFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateForm(w, "Shorten URL", "s", [][]string{{"url", r.FormValue("url")}, {"slug", r.FormValue("slug")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func UpdateFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateForm(w, "Update short link", "u", [][]string{{"slug", r.FormValue("slug")}, {"new", r.FormValue("new")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func DeleteFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateForm(w, "Delete short link", "d", [][]string{{"slug", r.FormValue("slug")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func generateForm(w http.ResponseWriter, title string, url string, fields [][]string) error {
	tmpl, err := template.New("Form").Parse("<!doctype html><html lang=en><meta name=viewport content=\"width=device-width, initial-scale=1.0\"><title>{{.Title}}</title><h1>{{.Title}}</h1><form action={{.Url}} method=post>{{range .Fields}}<input type=text name={{index . 0}} placeholder={{index . 0}} value=\"{{index . 1}}\"><br><br>{{end}}<input type=submit value={{.Title}}></form></html>")
	if err != nil {
		return err
	}
	err = tmpl.Execute(w, &struct {
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

	if err, e := slugExists(slug); !e || err != nil {
		http.Error(w, "Slug not found", http.StatusNotFound)
		return
	}

	_, err := db.Exec("UPDATE redirect SET url = ? WHERE slug = ?", newUrl, slug)
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
	tmpl, err := template.New("List").Parse("<!doctype html><html lang=en><meta name=viewport content=\"width=device-width, initial-scale=1.0\"><title>Short URLs</title><h1>Short URLs</h1><table><tr><th>slug</th><th>url</th><th>hits</th></tr>{{range .}}<tr><td>{{.Slug}}</td><td>{{.Url}}</td><td>{{.Hits}}</td></tr>{{end}}</table></html>")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, &list)
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
	err := db.QueryRow("SELECT url FROM redirect WHERE slug = ?", slug).Scan(&redirectUrl)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	go func() {
		_, _ = db.Exec("UPDATE redirect SET hits = hits + 1 WHERE slug = ?", slug)
	}()

	http.Redirect(w, r, redirectUrl, http.StatusTemporaryRedirect)
}

func CatchAllHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, viper.GetString("defaultUrl"), http.StatusTemporaryRedirect)
}
