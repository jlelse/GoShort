package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite/sqlitex"
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

func TestListSorting(t *testing.T) {
	t.Run("Test list sorting", func(t *testing.T) {
		app := testApp(t)
		defer closeTestApp(t, app)
		app.config.Password = "abc"

		require.NoError(t, app.insertRedirect("a", "https://a.example", typUrl))
		require.NoError(t, app.insertRedirect("m", "https://m.example", typUrl))
		require.NoError(t, app.insertRedirect("z", "https://z.example", typUrl))

		// set created timestamps
		conn, err := app.dbpool.Take(context.Background())
		require.NoError(t, err)
		err = sqlitex.Execute(conn, "UPDATE redirect SET created = ? WHERE slug = ?", &sqlitex.ExecOptions{Args: []any{time.Now().Unix() - 300, "a"}})
		require.NoError(t, err)
		err = sqlitex.Execute(conn, "UPDATE redirect SET created = ? WHERE slug = ?", &sqlitex.ExecOptions{Args: []any{time.Now().Unix() - 100, "m"}})
		require.NoError(t, err)
		err = sqlitex.Execute(conn, "UPDATE redirect SET created = ? WHERE slug = ?", &sqlitex.ExecOptions{Args: []any{time.Now().Unix() - 10, "z"}})
		require.NoError(t, err)
		app.dbpool.Put(conn)

		router := app.initRouter()

		// default sort (created desc): z, m, a
		req := httptest.NewRequest("GET", "http://example.com/l?password=abc", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s := string(body)
		idxZ := strings.Index(s, "<td>z</td>")
		idxM := strings.Index(s, "<td>m</td>")
		idxA := strings.Index(s, "<td>a</td>")
		assert.True(t, idxZ < idxM && idxM < idxA)

		// sort by slug: a, m, z
		req = httptest.NewRequest("GET", "http://example.com/l?password=abc&sort=slug", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp = w.Result()
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s = string(body)
		idxA = strings.Index(s, "<td>a</td>")
		idxM = strings.Index(s, "<td>m</td>")
		idxZ = strings.Index(s, "<td>z</td>")
		assert.True(t, idxA < idxM && idxM < idxZ)

		// set hits and test hits sort (desc)
		conn, err = app.dbpool.Take(context.Background())
		require.NoError(t, err)
		err = sqlitex.Execute(conn, "UPDATE redirect SET hits = ? WHERE slug = ?", &sqlitex.ExecOptions{Args: []any{10, "a"}})
		require.NoError(t, err)
		err = sqlitex.Execute(conn, "UPDATE redirect SET hits = ? WHERE slug = ?", &sqlitex.ExecOptions{Args: []any{5, "m"}})
		require.NoError(t, err)
		err = sqlitex.Execute(conn, "UPDATE redirect SET hits = ? WHERE slug = ?", &sqlitex.ExecOptions{Args: []any{50, "z"}})
		require.NoError(t, err)
		app.dbpool.Put(conn)

		req = httptest.NewRequest("GET", "http://example.com/l?password=abc&sort=hits", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp = w.Result()
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s = string(body)
		idxZ = strings.Index(s, "<td>z</td>")
		idxA = strings.Index(s, "<td>a</td>")
		idxM = strings.Index(s, "<td>m</td>")
		assert.True(t, idxZ < idxA && idxA < idxM)

		// sort by url: alphabetical a,m,z
		req = httptest.NewRequest("GET", "http://example.com/l?password=abc&sort=url", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp = w.Result()
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s = string(body)
		idxA = strings.Index(s, "<td>https://a.example</td>")
		idxM = strings.Index(s, "<td>https://m.example</td>")
		idxZ = strings.Index(s, "<td>https://z.example</td>")
		assert.True(t, idxA < idxM && idxM < idxZ)

		// sort by slug desc (toggle dir)
		req = httptest.NewRequest("GET", "http://example.com/l?password=abc&sort=slug&dir=desc", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp = w.Result()
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s = string(body)
		idxZ = strings.Index(s, "<td>z</td>")
		idxM = strings.Index(s, "<td>m</td>")
		idxA = strings.Index(s, "<td>a</td>")
		assert.True(t, idxZ < idxM && idxM < idxA)

		// sort by hits asc (toggle direction)
		req = httptest.NewRequest("GET", "http://example.com/l?password=abc&sort=hits&dir=asc", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp = w.Result()
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s = string(body)
		idxA = strings.Index(s, "<td>a</td>")
		idxM = strings.Index(s, "<td>m</td>")
		idxZ = strings.Index(s, "<td>z</td>")
		// hits ascending: m (5), a (10), z (50)
		assert.True(t, idxM < idxA && idxA < idxZ)
	})
}

func TestListUI(t *testing.T) {
	t.Run("List UI elements", func(t *testing.T) {
		app := testApp(t)
		defer closeTestApp(t, app)
		app.config.Password = "abc"
		app.config.ShortUrl = "https://short.example.com"

		require.NoError(t, app.insertRedirect("x", "https://x.example", typUrl))

		router := app.initRouter()

		// default page
		req := httptest.NewRequest("GET", "http://example.com/l?password=abc", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s := string(body)

		// full short url present in copy link (slashes are escaped in HTML attribute)
		assert.Contains(t, s, "short.example.com\\/x")
		assert.Contains(t, s, "onclick=\"copyText('")
		assert.Contains(t, s, "short.example.com\\/x")

		// delete link present
		assert.Contains(t, s, "href=\"/d?slug=x\"")

		// headers capitalized and actions header
		assert.Contains(t, s, "<th>Actions</th>")
		assert.Contains(t, s, "<a href=\"/l?sort=slug&dir=asc\">Slug")
		assert.Contains(t, s, "<a href=\"/l?sort=hits&dir=desc\">Hits")
		assert.Contains(t, s, "<a href=\"/l?sort=url&dir=asc\">URL")

		// clicking sort=slug shows indicator and toggles to desc
		req = httptest.NewRequest("GET", "http://example.com/l?password=abc&sort=slug", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp = w.Result()
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s = string(body)
		assert.Contains(t, s, "Slug ↑")
		assert.Contains(t, s, "href=\"/l?sort=slug&dir=desc\"")

		// hits active shows down arrow and toggles to asc
		req = httptest.NewRequest("GET", "http://example.com/l?password=abc&sort=hits", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp = w.Result()
		body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		s = string(body)
		assert.Contains(t, s, "Hits ↓")
		assert.Contains(t, s, "href=\"/l?sort=hits&dir=asc\"")
	})
}
