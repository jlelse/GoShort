package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testApp(t *testing.T) *app {
	app := &app{
		config: &config{
			DBPath: filepath.Join(t.TempDir(), "data.db"),
		},
	}
	err := app.openDatabase()
	require.NoError(t, err)
	return app
}

func closeTestApp(_ *testing.T, app *app) {
	app.shutdown.ShutdownAndWait()
}

func Test_slugExists(t *testing.T) {
	t.Run("Test slugs", func(t *testing.T) {
		app := testApp(t)

		exists, err := app.slugExists("source")
		assert.NoError(t, err)
		assert.True(t, exists)
		exists, err = app.slugExists("test")
		assert.NoError(t, err)
		assert.False(t, exists)

		closeTestApp(t, app)
	})
}

func Test_generateSlug(t *testing.T) {
	t.Run("Test slug generation", func(t *testing.T) {
		assert.Len(t, generateSlug(), 6)
	})
}

func TestShortenedUrlHandler(t *testing.T) {
	t.Run("Test ShortenedUrlHandler", func(t *testing.T) {
		app := testApp(t)

		app.config.DefaultUrl = "http://long.example.com"

		router := app.initRouter()

		t.Run("Test redirect code", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/source", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
		})
		t.Run("Test default redirect location header", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/source", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, "https://github.com/jlelse/GoShort", resp.Header.Get("Location"))
		})
		t.Run("Test missing slug redirect code", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
		t.Run("Test no slug mux var", func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
			assert.Equal(t, "http://long.example.com", resp.Header.Get("Location"))
		})
		t.Run("Test custom url redirect", func(t *testing.T) {
			err := app.insertRedirect("customurl", "https://example.net", typUrl)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "http://example.com/customurl", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()

			assert.Equal(t, "https://example.net", resp.Header.Get("Location"))
		})
		t.Run("Test custom text", func(t *testing.T) {
			err := app.insertRedirect("customtext", "Hello!", typText)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "http://example.com/customtext", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()

			respBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			_ = resp.Body.Close()

			assert.Equal(t, "Hello!", string(respBody))
		})

		closeTestApp(t, app)
	})
}

func Test_checkPassword(t *testing.T) {
	app := testApp(t)

	app.config.Password = "abc"

	t.Run("No password", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)

		assert.False(t, app.checkPassword(httptest.NewRecorder(), req))
	})
	t.Run("Password via query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test?password=abc", nil)

		assert.True(t, app.checkPassword(httptest.NewRecorder(), req))
	})
	t.Run("Wrong password via query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test?password=wrong", nil)

		assert.False(t, app.checkPassword(httptest.NewRecorder(), req))
	})
	t.Run("Password via BasicAuth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.SetBasicAuth("username", "abc")

		assert.True(t, app.checkPassword(httptest.NewRecorder(), req))
	})
	t.Run("Wrong password via BasicAuth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.SetBasicAuth("username", "wrong")

		assert.False(t, app.checkPassword(httptest.NewRecorder(), req))
	})

	t.Run("Test login middleware success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.SetBasicAuth("username", "abc")

		w := httptest.NewRecorder()
		app.loginMiddleware(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusNotModified)
		})).ServeHTTP(w, req)
		resp := w.Result()

		assert.Equal(t, http.StatusNotModified, resp.StatusCode)
	})
	t.Run("Test login middleware fail", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		req.SetBasicAuth("username", "xyz")

		w := httptest.NewRecorder()
		app.loginMiddleware(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusNotModified)
		})).ServeHTTP(w, req)
		resp := w.Result()

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	closeTestApp(t, app)
}
