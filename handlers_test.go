package main

import (
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func configuredTestApp(t *testing.T) *app {
	t.Helper()
	a := testApp(t)
	a.config.Password = "pw"
	a.config.ShortUrl = "http://sho.rt"
	a.config.DefaultUrl = "http://default.test"
	return a
}

func getRedirect(t *testing.T, a *app, slug string) (url string, typ string, hits int, found bool) {
	t.Helper()
	conn, err := a.dbpool.Take(context.Background())
	require.NoError(t, err)
	defer a.dbpool.Put(conn)
	err = sqlitex.Execute(conn, "SELECT url, type, hits FROM redirect WHERE slug = ?", &sqlitex.ExecOptions{
		Args: []any{slug},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			url = stmt.ColumnText(0)
			typ = stmt.ColumnText(1)
			hits = stmt.ColumnInt(2)
			found = true
			return nil
		},
	})
	require.NoError(t, err)
	return
}

func TestOpenDatabaseEmptyPath(t *testing.T) {
	app := &app{config: &config{}}
	err := app.openDatabase()
	assert.Error(t, err)
}

func TestDatabaseHelpersErrorPaths(t *testing.T) {
	app := configuredTestApp(t)
	app.shutdown.ShutdownAndWait()

	assert.Error(t, app.insertRedirect("fail", "http://example.com", typUrl))
	assert.Error(t, app.updateSlug(context.Background(), "http://example.com", typUrl, "fail"))
	assert.Error(t, app.deleteSlug("fail"))
	_, err := app.slugExists("fail")
	assert.Error(t, err)
}

func TestFormHandlers(t *testing.T) {
	req := httptest.NewRequest("GET", "/s?url=http://example.com&slug=abc", nil)
	w := httptest.NewRecorder()
	shortenFormHandler(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Contains(t, string(body), "Shorten URL")

	req = httptest.NewRequest("GET", "/u?slug=abc&type=url&new=http://example.com/new", nil)
	w = httptest.NewRecorder()
	updateFormHandler(w, req)
	resp = w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Contains(t, string(body), "Update short link")

	req = httptest.NewRequest("GET", "/ut?slug=abc&new=newtext", nil)
	w = httptest.NewRecorder()
	updateTextFormHandler(w, req)
	resp = w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Contains(t, string(body), "Update text")

	req = httptest.NewRequest("GET", "/d?slug=abc", nil)
	w = httptest.NewRecorder()
	deleteFormHandler(w, req)
	resp = w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Contains(t, string(body), "Delete short link")

	req = httptest.NewRequest("GET", "/t?slug=abc&text=hello", nil)
	w = httptest.NewRecorder()
	shortenTextFormHandler(w, req)
	resp = w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Contains(t, string(body), "Save text")
}

func TestFormHandlersTemplateErrors(t *testing.T) {
	t.Run("url templates fail", func(t *testing.T) {
		original := urlFormTemplate
		urlFormTemplate = template.Must(template.New("fail").Funcs(template.FuncMap{
			"fail": func() (string, error) { return "", errors.New("fail") },
		}).Parse("{{fail}}"))
		defer func() { urlFormTemplate = original }()

		req := httptest.NewRequest("GET", "/s", nil)
		w := httptest.NewRecorder()
		shortenFormHandler(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)

		req = httptest.NewRequest("GET", "/u", nil)
		w = httptest.NewRecorder()
		updateFormHandler(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)

		req = httptest.NewRequest("GET", "/d", nil)
		w = httptest.NewRecorder()
		deleteFormHandler(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
	})

	t.Run("text templates fail", func(t *testing.T) {
		original := textFormTemplate
		textFormTemplate = template.Must(template.New("fail").Funcs(template.FuncMap{
			"fail": func() (string, error) { return "", errors.New("fail") },
		}).Parse("{{fail}}"))
		defer func() { textFormTemplate = original }()

		req := httptest.NewRequest("GET", "/ut", nil)
		w := httptest.NewRecorder()
		updateTextFormHandler(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)

		req = httptest.NewRequest("GET", "/t", nil)
		w = httptest.NewRecorder()
		shortenTextFormHandler(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
	})
}

func TestShortenHandlerScenarios(t *testing.T) {
	t.Run("missing url parameter", func(t *testing.T) {
		app := configuredTestApp(t)
		router := app.initRouter()

		req := httptest.NewRequest("POST", "/s?password=pw", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		closeTestApp(t, app)
	})

	t.Run("manual slug success and conflict", func(t *testing.T) {
		app := configuredTestApp(t)
		router := app.initRouter()

		body := strings.NewReader("url=http://example.com&slug=abc")
		req := httptest.NewRequest("POST", "/s?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		assert.Contains(t, string(respBody), "http://sho.rt/abc")

		body = strings.NewReader("url=http://example.net&slug=abc")
		req = httptest.NewRequest("POST", "/s?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		closeTestApp(t, app)
	})

	t.Run("reuse existing url slug", func(t *testing.T) {
		app := configuredTestApp(t)
		require.NoError(t, app.insertRedirect("reuse", "http://reuse.example", typUrl))
		router := app.initRouter()

		body := strings.NewReader("url=http://reuse.example")
		req := httptest.NewRequest("POST", "/s?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, string(respBody), "http://sho.rt/reuse")
		closeTestApp(t, app)
	})

	t.Run("auto slug generation", func(t *testing.T) {
		app := configuredTestApp(t)
		router := app.initRouter()

		body := strings.NewReader("url=http://auto.example")
		req := httptest.NewRequest("POST", "/s?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Contains(t, string(respBody), "http://sho.rt/")
		closeTestApp(t, app)
	})
}

func TestShortenTextHandlerScenarios(t *testing.T) {
	t.Run("missing text parameter", func(t *testing.T) {
		app := configuredTestApp(t)
		router := app.initRouter()

		req := httptest.NewRequest("POST", "/t?password=pw", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		closeTestApp(t, app)
	})

	t.Run("manual slug success and conflict", func(t *testing.T) {
		app := configuredTestApp(t)
		router := app.initRouter()

		body := strings.NewReader("text=hello&slug=textslug")
		req := httptest.NewRequest("POST", "/t?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		assert.Contains(t, string(respBody), "http://sho.rt/textslug")

		body = strings.NewReader("text=other&slug=textslug")
		req = httptest.NewRequest("POST", "/t?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
		closeTestApp(t, app)
	})

	t.Run("reuse existing text slug", func(t *testing.T) {
		app := configuredTestApp(t)
		require.NoError(t, app.insertRedirect("textreuse", "hello", typText))
		router := app.initRouter()

		body := strings.NewReader("text=hello")
		req := httptest.NewRequest("POST", "/t?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, string(respBody), "http://sho.rt/textreuse")
		closeTestApp(t, app)
	})

	t.Run("auto slug generation", func(t *testing.T) {
		app := configuredTestApp(t)
		router := app.initRouter()

		body := strings.NewReader("text=autotext")
		req := httptest.NewRequest("POST", "/t?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := w.Result()
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)
		assert.Contains(t, string(respBody), "http://sho.rt/")
		closeTestApp(t, app)
	})
}

func TestShortenHandlersSlugExistsError(t *testing.T) {
	app := configuredTestApp(t)
	router := app.initRouter()
	app.shutdown.ShutdownAndWait()

	body := strings.NewReader("url=http://example.com")
	req := httptest.NewRequest("POST", "/s?password=pw", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)

	body = strings.NewReader("text=hello")
	req = httptest.NewRequest("POST", "/t?password=pw", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
}

func TestUpdateHandler(t *testing.T) {
	t.Run("missing fields and not found", func(t *testing.T) {
		app := configuredTestApp(t)
		router := app.initRouter()

		req := httptest.NewRequest("POST", "/u?password=pw", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		req = httptest.NewRequest("POST", "/u?password=pw&slug=a", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		req = httptest.NewRequest("POST", "/u?password=pw&slug=missing&new=x", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)

		closeTestApp(t, app)
	})

	t.Run("successful updates", func(t *testing.T) {
		app := configuredTestApp(t)
		require.NoError(t, app.insertRedirect("update", "http://old.example", typUrl))
		router := app.initRouter()

		body := strings.NewReader("slug=update&new=http://new.example")
		req := httptest.NewRequest("POST", "/u?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusAccepted, w.Result().StatusCode)
		u, typ, _, found := getRedirect(t, app, "update")
		assert.True(t, found)
		assert.Equal(t, "http://new.example", u)
		assert.Equal(t, typUrl, typ)

		body = strings.NewReader("slug=update&new=updated text&type=text")
		req = httptest.NewRequest("POST", "/u?password=pw", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusAccepted, w.Result().StatusCode)
		u, typ, _, found = getRedirect(t, app, "update")
		assert.True(t, found)
		assert.Equal(t, "updated text", u)
		assert.Equal(t, typText, typ)

		closeTestApp(t, app)
	})
}

func TestUpdateHandlerSlugExistsError(t *testing.T) {
	app := configuredTestApp(t)
	router := app.initRouter()
	app.shutdown.ShutdownAndWait()

	body := strings.NewReader("slug=any&new=value")
	req := httptest.NewRequest("POST", "/u?password=pw", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)
}

func TestUpdateHandlerUpdateError(t *testing.T) {
	app := configuredTestApp(t)
	require.NoError(t, app.insertRedirect("updateerr", "http://old.example", typUrl))
	router := app.initRouter()

	body := strings.NewReader("slug=updateerr&new=http://new.example")
	req := httptest.NewRequest("POST", "/u?password=pw", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
}

func TestDeleteHandler(t *testing.T) {
	t.Run("missing or unknown slug", func(t *testing.T) {
		app := configuredTestApp(t)
		router := app.initRouter()

		req := httptest.NewRequest("POST", "/d?password=pw", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)

		req = httptest.NewRequest("POST", "/d?password=pw&slug=unknown", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)

		closeTestApp(t, app)
	})

	t.Run("successful delete", func(t *testing.T) {
		app := configuredTestApp(t)
		require.NoError(t, app.insertRedirect("todelete", "http://delete.example", typUrl))
		router := app.initRouter()

		req := httptest.NewRequest("POST", "/d?password=pw&slug=todelete", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusAccepted, w.Result().StatusCode)

		_, _, _, found := getRedirect(t, app, "todelete")
		assert.False(t, found)
		closeTestApp(t, app)
	})
}

func TestDeleteHandlerSlugExistsError(t *testing.T) {
	app := configuredTestApp(t)
	router := app.initRouter()
	app.shutdown.ShutdownAndWait()

	req := httptest.NewRequest("POST", "/d?password=pw&slug=any", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)
}

func TestListHandler(t *testing.T) {
	app := configuredTestApp(t)
	require.NoError(t, app.insertRedirect("one", "http://one.example", typUrl))
	require.NoError(t, app.insertRedirect("two", "hello", typText))
	router := app.initRouter()

	req := httptest.NewRequest("GET", "/l?password=pw", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "one")
	assert.Contains(t, string(body), "two")
	closeTestApp(t, app)
}

func TestListHandlerError(t *testing.T) {
	app := configuredTestApp(t)
	router := app.initRouter()
	app.shutdown.ShutdownAndWait()

	req := httptest.NewRequest("GET", "/l?password=pw", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
}

func TestListHandlerTemplateError(t *testing.T) {
	app := configuredTestApp(t)
	router := app.initRouter()

	original := listTemplate
	listTemplate = template.Must(template.New("fail").Funcs(template.FuncMap{
		"fail": func() (string, error) { return "", errors.New("fail") },
	}).Parse("{{fail}}"))
	defer func() { listTemplate = original }()

	req := httptest.NewRequest("GET", "/l?password=pw", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
}

func TestShortenedURLHandlerIncrementsHits(t *testing.T) {
	app := configuredTestApp(t)
	require.NoError(t, app.insertRedirect("hitme", "http://hits.example", typUrl))
	router := app.initRouter()

	req := httptest.NewRequest("GET", "/hitme", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	time.Sleep(50 * time.Millisecond)

	_, _, hits, found := getRedirect(t, app, "hitme")
	assert.True(t, found)
	assert.GreaterOrEqual(t, hits, 1)
	closeTestApp(t, app)
}

func TestMainFunctionShutsDownOnSignal(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	dbPath := filepath.Join(t.TempDir(), "data", "main.db")
	viper.Set("dbPath", dbPath)
	viper.Set("password", "pw")
	viper.Set("shortUrl", "http://sho.rt")
	viper.Set("defaultUrl", "http://default.test")
	viper.Set("port", 0)

	done := make(chan struct{})
	go func() {
		main()
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	p, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, p.Signal(os.Interrupt))

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("main did not exit after interrupt")
	}
}

func TestMainMissingPassword(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	originalFatal := fatalf
	originalExit := exitFunc
	defer func() {
		fatalf = originalFatal
		exitFunc = originalExit
	}()

	called := 0
	fatalf = func(...any) { called++ }
	exitFunc = func(int) { called++ }

	viper.Set("shortUrl", "http://sho.rt")
	viper.Set("defaultUrl", "http://default.test")
	viper.Set("dbPath", filepath.Join(t.TempDir(), "data.db"))

	main()
	assert.Equal(t, 1, called)
}

func TestMainMissingShortURL(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	originalFatal := fatalf
	originalExit := exitFunc
	defer func() {
		fatalf = originalFatal
		exitFunc = originalExit
	}()

	called := 0
	fatalf = func(...any) { called++ }
	exitFunc = func(int) { called++ }

	viper.Set("password", "pw")
	viper.Set("defaultUrl", "http://default.test")
	viper.Set("dbPath", filepath.Join(t.TempDir(), "data.db"))

	main()
	assert.Equal(t, 1, called)
}

func TestMainMissingDefaultURL(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	originalFatal := fatalf
	originalExit := exitFunc
	defer func() {
		fatalf = originalFatal
		exitFunc = originalExit
	}()

	called := 0
	fatalf = func(...any) { called++ }
	exitFunc = func(int) { called++ }

	viper.Set("password", "pw")
	viper.Set("shortUrl", "http://sho.rt")
	viper.Set("dbPath", filepath.Join(t.TempDir(), "data.db"))

	main()
	assert.Equal(t, 1, called)
}

func TestMainDatabaseError(t *testing.T) {
	viper.Reset()
	defer viper.Reset()

	originalFatal := fatalf
	originalExit := exitFunc
	defer func() {
		fatalf = originalFatal
		exitFunc = originalExit
	}()

	exitCode := 0
	fatalf = func(...any) {}
	exitFunc = func(code int) { exitCode = code }

	viper.Set("password", "pw")
	viper.Set("shortUrl", "http://sho.rt")
	viper.Set("defaultUrl", "http://default.test")
	viper.Set("dbPath", "")
	viper.Set("port", 0)

	main()
	assert.Equal(t, 1, exitCode)
}
