package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	gsd "git.jlel.se/jlelse/go-shutdowner"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/viper"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

type app struct {
	config   *config
	dbpool   *sqlitex.Pool
	write    sync.Mutex
	shutdown gsd.Shutdowner
}

type config struct {
	Port       int    `mapstructure:"port"`
	DBPath     string `mapstructure:"dbPath"`
	Password   string `mapstructure:"password"`
	ShortUrl   string `mapstructure:"shortUrl"`
	DefaultUrl string `mapstructure:"defaultUrl"`
}

func (a *app) initRouter() (router *chi.Mux) {
	router = chi.NewMux()
	router.Use(middleware.GetHead)
	router.Group(func(r chi.Router) {
		r.Use(a.loginMiddleware)
		r.Get("/s", shortenFormHandler)
		r.Post("/s", a.shortenHandler)
		r.Get("/t", shortenTextFormHandler)
		r.Post("/t", a.shortenTextHandler)
		r.Get("/u", updateFormHandler)
		r.Get("/ut", updateTextFormHandler)
		r.Post("/u", a.updateHandler)
		r.Get("/d", deleteFormHandler)
		r.Post("/d", a.deleteHandler)
		r.Get("/l", a.listHandler)
	})
	router.Get("/{slug}", a.shortenedURLHandler)
	router.Get("/", a.defaultURLRedirectHandler)
	return
}

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	viper.SetDefault("dbPath", "data/goshort.db")
	viper.SetDefault("port", 8080)

	viper.SetConfigName("config")
	viper.AddConfigPath("./config")
	viper.AddConfigPath(".")
	_ = viper.ReadInConfig()

	if !viper.IsSet("password") {
		log.Fatal("No password (password) is configured.")
		return
	}
	if !viper.IsSet("shortUrl") {
		log.Fatal("No short URL (shortUrl) is configured.")
		return
	}
	if !viper.IsSet("defaultUrl") {
		log.Fatal("No default URL (defaultUrl) is configured.")
		return
	}

	app := &app{}

	app.config = &config{}
	err := viper.Unmarshal(app.config)
	if err != nil {
		log.Fatal("Failed to unmarshal config:", err.Error())
		return
	}

	err = app.openDatabase()
	if err != nil {
		log.Println("Error opening database:", err.Error())
		app.shutdown.ShutdownAndWait()
		os.Exit(1)
		return
	}

	httpServer := &http.Server{
		Addr:         ":" + strconv.Itoa(app.config.Port),
		Handler:      app.initRouter(),
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 5 * time.Minute,
	}
	app.shutdown.Add(func() {
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

	app.shutdown.Wait()
}

func (a *app) loginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.checkPassword(w, r) {
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

func (a *app) shortenHandler(w http.ResponseWriter, r *http.Request) {
	writeShortenedURL := func(w http.ResponseWriter, slug string) {
		_, _ = w.Write([]byte(a.config.ShortUrl + "/" + slug))
	}

	requestURL := r.FormValue("url")
	if requestURL == "" {
		http.Error(w, "url parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.FormValue("slug")
	manualSlug := false
	if slug == "" {
		conn := a.dbpool.Get(r.Context())
		defer a.dbpool.Put(conn)
		_ = sqlitex.Exec(conn, "SELECT slug FROM redirect WHERE url = ?", func(stmt *sqlite.Stmt) error {
			slug = stmt.ColumnText(0)
			return nil
		}, requestURL)
	} else {
		manualSlug = true
	}

	if slug != "" {
		if e, _ := a.slugExists(slug); e {
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
			exists, err = a.slugExists(slug)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	if err := a.insertRedirect(slug, requestURL, typUrl); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeShortenedURL(w, slug)
}

func (a *app) shortenTextHandler(w http.ResponseWriter, r *http.Request) {
	writeShortenedURL := func(w http.ResponseWriter, slug string) {
		_, _ = w.Write([]byte(a.config.ShortUrl + "/" + slug))
	}

	requestText := r.FormValue("text")
	if requestText == "" {
		http.Error(w, "text parameter not set", http.StatusBadRequest)
		return
	}

	slug := r.FormValue("slug")
	manualSlug := false
	if slug == "" {
		conn := a.dbpool.Get(r.Context())
		defer a.dbpool.Put(conn)
		_ = sqlitex.Exec(conn, "SELECT slug FROM redirect WHERE url = ? and type = 'text'", func(stmt *sqlite.Stmt) error {
			slug = stmt.ColumnText(0)
			return nil
		}, requestText)
	} else {
		manualSlug = true
	}

	if slug != "" {
		if e, _ := a.slugExists(slug); e {
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
			exists, err = a.slugExists(slug)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	if err := a.insertRedirect(slug, requestText, typText); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeShortenedURL(w, slug)
}

func (a *app) updateHandler(w http.ResponseWriter, r *http.Request) {
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

	if e, err := a.slugExists(slug); err != nil || !e {
		http.NotFound(w, r)
		return
	}

	if err := a.updateSlug(r.Context(), newURL, typeString, slug); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Slug updated"))
}

func (a *app) deleteHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.FormValue("slug")
	if slug == "" {
		http.Error(w, "Specify the slug to delete", http.StatusBadRequest)
		return
	}

	if e, err := a.slugExists(slug); !e || err != nil {
		http.NotFound(w, r)
		return
	}

	if err := a.deleteSlug(slug); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Slug deleted"))
}

func (a *app) listHandler(w http.ResponseWriter, r *http.Request) {
	type row struct {
		Slug string
		URL  string
		Hits int
	}
	var list []row

	conn := a.dbpool.Get(r.Context())
	err := sqlitex.Exec(conn, "SELECT slug, url, hits FROM redirect", func(stmt *sqlite.Stmt) error {
		var r row
		r.Slug = stmt.ColumnText(0)
		r.URL = stmt.ColumnText(1)
		r.Hits = stmt.ColumnInt(2)
		list = append(list, r)
		return nil
	})
	a.dbpool.Put(conn)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = listTemplate.Execute(w, &list)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (a *app) checkPassword(w http.ResponseWriter, r *http.Request) bool {
	// Check basic auth
	if _, pass, ok := r.BasicAuth(); ok && pass == a.config.Password {
		return true
	}
	// Check query or form param
	if r.FormValue("password") == a.config.Password {
		return true
	}
	// Require password
	w.Header().Set("WWW-Authenticate", `Basic realm="Please enter a password!"`)
	http.Error(w, "Not authenticated", http.StatusUnauthorized)
	return false
}

func generateSlug() string {
	var chars = []rune("0123456789abcdefghijklmnopqrstuvwxyz")
	s := make([]rune, 6)
	for i := range s {
		s[i] = chars[rand.Intn(len(chars))]
	}
	return string(s)
}

func (a *app) shortenedURLHandler(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	var redirectURL, typeString string

	conn := a.dbpool.Get(r.Context())
	err := sqlitex.Exec(conn, "SELECT url, type FROM redirect WHERE slug = ? LIMIT 1", func(stmt *sqlite.Stmt) error {
		redirectURL = stmt.ColumnText(0)
		typeString = stmt.ColumnText(1)
		return nil
	}, slug)
	a.dbpool.Put(conn)

	if err != nil || redirectURL == "" || typeString == "" {
		http.NotFound(w, r)
		return
	}

	a.increaseHits(slug)

	switch typeString {
	case typText:
		_, _ = w.Write([]byte(redirectURL))
	default:
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
	}
}

func (a *app) defaultURLRedirectHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, a.config.DefaultUrl, http.StatusTemporaryRedirect)
}
