package main

import (
	"database/sql"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupFakeDB(t *testing.T) {
	var err error
	db, err = sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	migrateDatabase()
}

func closeFakeDB(t *testing.T) {
	err := db.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func Test_slugExists(t *testing.T) {
	t.Run("Test slugs", func(t *testing.T) {
		setupFakeDB(t)
		if err, exists := slugExists("source"); exists == false || err != nil {
			t.Error("Wrong slug existence")
		}
		if err, exists := slugExists("test"); exists == true || err != nil {
			t.Error("Wrong slug existence")
		}
		closeFakeDB(t)
	})
}

func Test_generateSlug(t *testing.T) {
	t.Run("Test slug generation", func(t *testing.T) {
		if slug := generateSlug(); len(slug) != 6 {
			t.Error("Wrong slug length")
		}
	})
}

func TestShortenedUrlHandler(t *testing.T) {
	viper.Set("defaultUrl", "http://long.example.com")
	t.Run("Test ShortenedUrlHandler", func(t *testing.T) {
		setupFakeDB(t)
		t.Run("Test redirect code", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/source", nil)
			req = mux.SetURLVars(req, map[string]string{"slug": "source"})
			w := httptest.NewRecorder()
			ShortenedUrlHandler(w, req)
			resp := w.Result()
			if resp.StatusCode != http.StatusTemporaryRedirect {
				t.Error()
			}
		})
		t.Run("Test redirect location header", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/source", nil)
			req = mux.SetURLVars(req, map[string]string{"slug": "source"})
			w := httptest.NewRecorder()
			ShortenedUrlHandler(w, req)
			resp := w.Result()
			if resp.Header.Get("Location") != "https://git.jlel.se/jlelse/GoShort" {
				t.Error()
			}
		})
		t.Run("Test missing slug redirect code", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/test", nil)
			req = mux.SetURLVars(req, map[string]string{"slug": "test"})
			w := httptest.NewRecorder()
			ShortenedUrlHandler(w, req)
			resp := w.Result()
			if resp.StatusCode != http.StatusNotFound {
				t.Error()
			}
		})
		t.Run("Test no slug mux var", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/", nil)
			w := httptest.NewRecorder()
			ShortenedUrlHandler(w, req)
			resp := w.Result()
			if resp.StatusCode != http.StatusTemporaryRedirect {
				t.Error()
			}
			if resp.Header.Get("Location") != "http://long.example.com" {
				t.Error()
			}
		})
		closeFakeDB(t)
	})
}

func Test_checkPassword(t *testing.T) {
	viper.Set("password", "abc")
	t.Run("No password", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		_ = req.ParseForm()
		if checkPassword(httptest.NewRecorder(), req) != false {
			t.Error()
		}
	})
	t.Run("Password via query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test?password=abc", nil)
		_ = req.ParseForm()
		if checkPassword(httptest.NewRecorder(), req) != true {
			t.Error()
		}
	})
	t.Run("Wrong password via query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test?password=wrong", nil)
		_ = req.ParseForm()
		if checkPassword(httptest.NewRecorder(), req) != false {
			t.Error()
		}
	})
	t.Run("Password via BasicAuth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.SetBasicAuth("username", "abc")
		if checkPassword(httptest.NewRecorder(), req) != true {
			t.Error()
		}
	})
	t.Run("Wrong password via BasicAuth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.SetBasicAuth("username", "wrong")
		if checkPassword(httptest.NewRecorder(), req) != false {
			t.Error()
		}
	})
}
