package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	goshutdowner "git.jlel.se/jlelse/go-shutdowner"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
)

var (
	appDb       *sql.DB
	appRouter   *chi.Mux
	dbWriteLock sync.Mutex
	shutdown    goshutdowner.Shutdowner
)

func initRouter() {
	appRouter = chi.NewRouter()
	appRouter.Use(middleware.GetHead)
	appRouter.Group(func(r chi.Router) {
		r.Use(loginMiddleware)
		r.Get("/s", shortenFormHandler)
		r.Post("/s", shortenHandler)
		r.Get("/t", shortenTextFormHandler)
		r.Post("/t", shortenTextHandler)
		r.Get("/u", updateFormHandler)
		r.Get("/ut", updateTextFormHandler)
		r.Post("/u", updateHandler)
		r.Get("/d", deleteFormHandler)
		r.Post("/d", deleteHandler)
		r.Get("/l", listHandler)
	})
	appRouter.Get("/{slug}", shortenedURLHandler)
	appRouter.Get("/", defaultURLRedirectHandler)
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
	dbPath := viper.GetString("dbPath")
	_ = os.MkdirAll(filepath.Dir(dbPath), 0644)
	appDb, err = sql.Open("sqlite3", dbPath+"?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=100")
	if err != nil {
		log.Println("Error opening database:", err.Error())
	}
	shutdown.Add(func() {
		_ = appDb.Close()
		log.Println("Closed database")
	})

	migrateDatabase()

	defer func() {
		_ = appDb.Close()
	}()

	initRouter()

	httpServer := &http.Server{
		Addr:         ":" + strconv.Itoa(viper.GetInt("port")),
		Handler:      appRouter,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 5 * time.Minute,
	}
	shutdown.Add(func() {
		toc, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if err := httpServer.Shutdown(toc); err != nil {
			log.Println("Error on server shutdown:", err.Error())
		}
		log.Println("Stopped server")
	})
	go func() {
		fmt.Println("Listening to " + httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Println("Failed to start HTTP server:", err.Error())
		}
	}()

	shutdown.Wait()
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
	if err := generateURLForm(w, "Shorten URL", "s", [][]string{{"url", r.FormValue("url")}, {"slug", r.FormValue("slug")}}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func updateFormHandler(w http.ResponseWriter, r *http.Request) {
	if err := generateURLForm(w, "Update short link", "u", [][]string{{"slug", r.FormValue("slug")}, {"type", "url"}, {"new", r.FormValue("new")}}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func updateTextFormHandler(w http.ResponseWriter, r *http.Request) {
	if err := generateTextForm(w, "Update text", "u", [][]string{{"slug", r.FormValue("slug")}, {"type", "text"}}, [][]string{{"new", r.FormValue("new")}}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func deleteFormHandler(w http.ResponseWriter, r *http.Request) {
	if err := generateURLForm(w, "Delete short link", "d", [][]string{{"slug", r.FormValue("slug")}}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func shortenTextFormHandler(w http.ResponseWriter, r *http.Request) {
	if err := generateTextForm(w, "Save text", "t", [][]string{{"slug", r.FormValue("slug")}}, [][]string{{"text", r.FormValue("text")}}); err != nil {
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

	requestURL := r.FormValue("url")
	if requestURL == "" {
		http.Error(w, "url parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.FormValue("slug")
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
		exists := true
		var err error
		for exists {
			slug = generateSlug()
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

	requestText := r.FormValue("text")
	if requestText == "" {
		http.Error(w, "text parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.FormValue("slug")
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
		exists := true
		var err error
		for exists {
			slug = generateSlug()
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
	slug := r.FormValue("slug")
	if slug == "" {
		http.Error(w, "Specify the slug to update", http.StatusBadRequest)
		return
	}

	newURL := r.FormValue("new")
	if newURL == "" {
		http.Error(w, "Specify the new URL", http.StatusBadRequest)
		return
	}

	typeString := r.FormValue("type")
	if typeString == "" {
		typeString = "url"
	}

	if e, err := slugExists(slug); err != nil || !e {
		http.NotFound(w, r)
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
	slug := r.FormValue("slug")
	if slug == "" {
		http.Error(w, "Specify the slug to delete", http.StatusBadRequest)
		return
	}

	if e, err := slugExists(slug); !e || err != nil {
		http.NotFound(w, r)
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
	if r.FormValue("password") == viper.GetString("password") {
		return true
	}
	if _, pass, ok := r.BasicAuth(); !ok || pass != viper.GetString("password") {
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

func defaultURLRedirectHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, viper.GetString("defaultUrl"), http.StatusTemporaryRedirect)
}
