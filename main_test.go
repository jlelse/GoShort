package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupFakeDB(t *testing.T) {
	var err error
	appDb, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "data.db")+"?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=100")
	if err != nil {
		t.Fatal(err)
	}
	migrateDatabase()
}

func closeFakeDB(t *testing.T) {
	err := appDb.Close()
	require.NoError(t, err)
}

func Test_slugExists(t *testing.T) {
	t.Run("Test slugs", func(t *testing.T) {
		setupFakeDB(t)

		exists, err := slugExists("source")
		assert.NoError(t, err)
		assert.True(t, exists)
		exists, err = slugExists("test")
		assert.NoError(t, err)
		assert.False(t, exists)

		closeFakeDB(t)
	})
}

func Test_generateSlug(t *testing.T) {
	t.Run("Test slug generation", func(t *testing.T) {
		assert.Len(t, generateSlug(), 6)
	})
}

func TestShortenedUrlHandler(t *testing.T) {
	viper.Set("defaultUrl", "http://long.example.com")
	t.Run("Test ShortenedUrlHandler", func(t *testing.T) {
		setupFakeDB(t)
		initRouter()
		t.Run("Test redirect code", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/source", nil)
			w := httptest.NewRecorder()
			appRouter.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
		})
		t.Run("Test redirect location header", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/source", nil)
			w := httptest.NewRecorder()
			appRouter.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, "https://git.jlel.se/jlelse/GoShort", resp.Header.Get("Location"))
		})
		t.Run("Test missing slug redirect code", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/test", nil)
			w := httptest.NewRecorder()
			appRouter.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
		t.Run("Test no slug mux var", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/", nil)
			w := httptest.NewRecorder()
			appRouter.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
			assert.Equal(t, "http://long.example.com", resp.Header.Get("Location"))
		})
		closeFakeDB(t)
	})
}

func Test_checkPassword(t *testing.T) {
	viper.Set("password", "abc")
	t.Run("No password", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)

		assert.False(t, checkPassword(httptest.NewRecorder(), req))
	})
	t.Run("Password via query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test?password=abc", nil)

		assert.True(t, checkPassword(httptest.NewRecorder(), req))
	})
	t.Run("Wrong password via query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test?password=wrong", nil)

		assert.False(t, checkPassword(httptest.NewRecorder(), req))
	})
	t.Run("Password via BasicAuth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.SetBasicAuth("username", "abc")

		assert.True(t, checkPassword(httptest.NewRecorder(), req))
	})
	t.Run("Wrong password via BasicAuth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.SetBasicAuth("username", "wrong")

		assert.False(t, checkPassword(httptest.NewRecorder(), req))
	})
}
