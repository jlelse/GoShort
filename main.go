package main

import (
	"database/sql"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rubenv/sql-migrate"
	"github.com/spf13/viper"
	"log"
	"math/rand"
	"net/http"
	"net/url"
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

	MigrateDatabase()

	defer func() {
		_ = db.Close()
	}()

	r := mux.NewRouter()
	r.HandleFunc("/s", ShortenHandler)
	r.HandleFunc("/d", DeleteHandler)
	r.HandleFunc("/{slug}", ShortenedUrlHandler)
	r.HandleFunc("/", CatchAllHandler)

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func MigrateDatabase() {
	migrations := &migrate.MemoryMigrationSource{
		Migrations: []*migrate.Migration{
			{
				Id:   "001",
				Up:   []string{"create table redirect(slug text not null primary key,url text not null,hits integer default 0 not null);insert into redirect (slug, url) values ('source', 'https://codeberg.org/jlelse/GoShort');"},
				Down: []string{"drop table redirect;"},
			},
		},
	}
	_, err := migrate.Exec(db, "sqlite3", migrations, migrate.Up)
	if err != nil {
		log.Fatal(err)
	}
}

func ShortenHandler(w http.ResponseWriter, r *http.Request) {
	if !checkPassword(w, r) {
		return
	}

	writeShortenedUrl := func(w http.ResponseWriter, slug string) {
		shortenedUrl, err := url.Parse(viper.GetString("shortUrl"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		shortenedUrl.Path = slug
		_, _ = w.Write([]byte(shortenedUrl.String()))
	}

	requestUrl := r.URL.Query().Get("url")
	if requestUrl == "" {
		http.Error(w, "url parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.URL.Query().Get("slug")
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

	stmt, err := db.Prepare("INSERT INTO redirect (slug, url, hits) VALUES (?, ?, ?)")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(slug, requestUrl, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeShortenedUrl(w, slug)
}

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !checkPassword(w, r) {
		return
	}

	slug := r.URL.Query().Get("slug")
	if slug == "" {
		http.Error(w, "Specify the slug to delete", http.StatusBadRequest)
		return
	}

	if err, e := slugExists(slug); !e || err != nil {
		http.Error(w, "Slug not found", http.StatusNotFound)
		return
	}

	stmt, err := db.Prepare("DELETE FROM redirect WHERE slug = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Slug deleted"))
}

func checkPassword(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("password") == viper.GetString("password") {
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

	stmt, err := db.Prepare("UPDATE redirect SET hits = hits + 1 WHERE slug = ?")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = stmt.Exec(slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, redirectUrl, http.StatusTemporaryRedirect)
}

func CatchAllHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, viper.GetString("defaultUrl"), http.StatusTemporaryRedirect)
}
