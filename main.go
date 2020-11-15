package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
)

var (
	appDb       *sql.DB
	appRouter   *chi.Mux
	dbWriteLock *sync.Mutex = &sync.Mutex{}
)

func initRouter() {
	appRouter = chi.NewRouter()
	appRouter.Use(middleware.GetHead)
	appRouter.With(loginMiddleware).Get("/s", shortenFormHandler)
	appRouter.With(loginMiddleware).Post("/s", shortenHandler)
	appRouter.With(loginMiddleware).Get("/t", shortenTextFormHandler)
	appRouter.With(loginMiddleware).Post("/t", shortenTextHandler)
	appRouter.With(loginMiddleware).Get("/u", updateFormHandler)
	appRouter.With(loginMiddleware).Get("/ut", updateTextFormHandler)
	appRouter.With(loginMiddleware).Post("/u", updateHandler)
	appRouter.With(loginMiddleware).Get("/d", deleteFormHandler)
	appRouter.With(loginMiddleware).Post("/d", deleteHandler)
	appRouter.With(loginMiddleware).Get("/l", listHandler)
	appRouter.HandleFunc("/{slug}", shortenedURLHandler)
	appRouter.NotFound(catchAllHandler)
}

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
	appDb, err = sql.Open("sqlite3", viper.GetString("dbPath")+"?cache=shared&mode=rwc&_journal_mode=WAL")
	if err != nil {
		log.Fatal(err)
	}

	migrateDatabase()

	defer func() {
		_ = appDb.Close()
	}()

	initRouter()

	addr := ":" + strconv.Itoa(viper.GetInt("port"))
	fmt.Println("Listening to " + addr)
	log.Fatal(http.ListenAndServe(addr, appRouter))
}

func loginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !checkPassword(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func shortenFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateURLForm(w, "Shorten URL", "s", [][]string{{"url", r.FormValue("url")}, {"slug", r.FormValue("slug")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func updateFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateURLForm(w, "Update short link", "u", [][]string{{"slug", r.FormValue("slug")}, {"type", "url"}, {"new", r.FormValue("new")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func updateTextFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateTextForm(w, "Update text", "u", [][]string{{"slug", r.FormValue("slug")}, {"type", "text"}}, [][]string{{"new", r.FormValue("new")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func deleteFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateURLForm(w, "Delete short link", "d", [][]string{{"slug", r.FormValue("slug")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func shortenTextFormHandler(w http.ResponseWriter, r *http.Request) {
	err := generateTextForm(w, "Save text", "t", [][]string{{"slug", r.FormValue("slug")}}, [][]string{{"text", r.FormValue("text")}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func generateURLForm(w http.ResponseWriter, title string, url string, fields [][]string) error {
	return urlFormTemplate.Execute(w, map[string]interface{}{
		"Title":  title,
		"URL":    url,
		"Fields": fields,
	})
}

func generateTextForm(w http.ResponseWriter, title string, url string, fields [][]string, textAreas [][]string) error {
	return textFormTemplate.Execute(w, map[string]interface{}{
		"Title":     title,
		"URL":       url,
		"Fields":    fields,
		"TextAreas": textAreas,
	})
}

func shortenHandler(w http.ResponseWriter, r *http.Request) {
	writeShortenedURL := func(w http.ResponseWriter, slug string) {
		_, _ = w.Write([]byte(viper.GetString("shortUrl") + "/" + slug))
	}

	_ = r.ParseForm()

	requestURL := r.Form.Get("url")
	if requestURL == "" {
		http.Error(w, "url parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.Form.Get("slug")
	manualSlug := false
	if slug == "" {
		_ = appDb.QueryRow("SELECT slug FROM redirect WHERE url = ?", requestURL).Scan(&slug)
	} else {
		manualSlug = true
	}

	if slug != "" {
		if e, _ := slugExists(slug); e {
			if manualSlug {
				http.Error(w, "slug already in use", http.StatusBadRequest)
				return
			}
			writeShortenedURL(w, slug)
			return
		}
	} else {
		var exists = true
		for exists == true {
			slug = generateSlug()
			var err error
			exists, err = slugExists(slug)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	dbWriteLock.Lock()
	if _, err := appDb.Exec("INSERT INTO redirect (slug, url) VALUES (?, ?)", slug, requestURL); err != nil {
		dbWriteLock.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dbWriteLock.Unlock()

	w.WriteHeader(http.StatusCreated)
	writeShortenedURL(w, slug)
}

func shortenTextHandler(w http.ResponseWriter, r *http.Request) {
	writeShortenedURL := func(w http.ResponseWriter, slug string) {
		_, _ = w.Write([]byte(viper.GetString("shortUrl") + "/" + slug))
	}

	_ = r.ParseForm()

	requestText := r.Form.Get("text")
	if requestText == "" {
		http.Error(w, "text parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.Form.Get("slug")
	manualSlug := false
	if slug == "" {
		_ = appDb.QueryRow("SELECT slug FROM redirect WHERE url = ? and type = 'text'", requestText).Scan(&slug)
	} else {
		manualSlug = true
	}

	if slug != "" {
		if e, _ := slugExists(slug); e {
			if manualSlug {
				http.Error(w, "slug already in use", http.StatusBadRequest)
				return
			}
			writeShortenedURL(w, slug)
			return
		}
	} else {
		var exists = true
		for exists == true {
			slug = generateSlug()
			var err error
			exists, err = slugExists(slug)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	dbWriteLock.Lock()
	if _, err := appDb.Exec("INSERT INTO redirect (slug, url, type) VALUES (?, ?, 'text')", slug, requestText); err != nil {
		dbWriteLock.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dbWriteLock.Unlock()

	w.WriteHeader(http.StatusCreated)
	writeShortenedURL(w, slug)
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	slug := r.Form.Get("slug")
	if slug == "" {
		http.Error(w, "Specify the slug to update", http.StatusBadRequest)
		return
	}

	newURL := r.Form.Get("new")
	if newURL == "" {
		http.Error(w, "Specify the new URL", http.StatusBadRequest)
		return
	}

	typeString := r.Form.Get("type")
	if typeString == "" {
		typeString = "url"
	}

	if e, err := slugExists(slug); err != nil || !e {
		http.Error(w, "Slug not found", http.StatusNotFound)
		return
	}

	dbWriteLock.Lock()
	if _, err := appDb.Exec("UPDATE redirect SET url = ?, type = ? WHERE slug = ?", newURL, typeString, slug); err != nil {
		dbWriteLock.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dbWriteLock.Unlock()

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Slug updated"))
}

func deleteHandler(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	slug := r.Form.Get("slug")
	if slug == "" {
		http.Error(w, "Specify the slug to delete", http.StatusBadRequest)
		return
	}

	if e, err := slugExists(slug); !e || err != nil {
		http.Error(w, "Slug not found", http.StatusNotFound)
		return
	}

	dbWriteLock.Lock()
	if _, err := appDb.Exec("DELETE FROM redirect WHERE slug = ?", slug); err != nil {
		dbWriteLock.Unlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dbWriteLock.Unlock()

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Slug deleted"))
}

func listHandler(w http.ResponseWriter, r *http.Request) {
	type row struct {
		Slug string
		URL  string
		Hits int
	}
	var list []row
	rows, err := appDb.Query("SELECT slug, url, hits FROM redirect")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var r row
		err = rows.Scan(&r.Slug, &r.URL, &r.Hits)
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
	_ = r.ParseForm()
	if r.Form.Get("password") == viper.GetString("password") {
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

func slugExists(slug string) (exists bool, err error) {
	err = appDb.QueryRow("SELECT EXISTS(SELECT 1 FROM redirect WHERE slug = ?)", slug).Scan(&exists)
	return
}

func shortenedURLHandler(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		catchAllHandler(w, r)
		return
	}

	var redirectURL, typeString string
	err := appDb.QueryRow("SELECT url, type FROM redirect WHERE slug = ?", slug).Scan(&redirectURL, &typeString)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	go func() {
		dbWriteLock.Lock()
		_, _ = appDb.Exec("UPDATE redirect SET hits = hits + 1 WHERE slug = ?", slug)
		dbWriteLock.Unlock()
	}()

	if typeString == "text" {
		_, _ = w.Write([]byte(redirectURL))
	} else {
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
	}
}

func catchAllHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, viper.GetString("defaultUrl"), http.StatusTemporaryRedirect)
}
