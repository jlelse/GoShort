package main

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

func (a *app) openDatabase() (err error) {
	if a.config.DBPath == "" {
		return errors.New("empty database path")
	}
	_ = os.MkdirAll(filepath.Dir(a.config.DBPath), os.ModePerm)
	a.dbpool, err = sqlitex.Open(a.config.DBPath, sqlite.OpenCreate|sqlite.OpenReadWrite|sqlite.OpenWAL|sqlite.OpenNoMutex, 10)
	if err != nil {
		return err
	}
	a.shutdown.Add(func() {
		_ = a.dbpool.Close()
		log.Println("Closed database")
	})
	a.migrateDatabase()
	return nil
}

func (a *app) migrateDatabase() {
	a.write.Lock()
	defer a.write.Unlock()

	schema := sqlitemigration.Schema{
		AppID: 0x1bd6d04a,
		Migrations: []string{
			`
			drop table if exists gorp_migrations;
			create table if not exists redirect(slug text not null primary key, url text not null, type text not null default 'url', hits integer default 0 not null);
			insert or replace into redirect (slug, url) values ('source', 'https://git.jlel.se/jlelse/GoShort');
			`,
		},
	}

	conn := a.dbpool.Get(context.Background())
	defer a.dbpool.Put(conn)
	err := sqlitemigration.Migrate(context.Background(), conn, schema)
	if err != nil {
		log.Fatal(err.Error())
		return
	}
}

const (
	typUrl  = "url"
	typText = "text"
)

func (a *app) insertRedirect(slug string, url string, typ string) error {
	a.write.Lock()
	defer a.write.Unlock()
	conn := a.dbpool.Get(context.Background())
	defer a.dbpool.Put(conn)
	return sqlitex.Execute(conn, "INSERT INTO redirect (slug, url, type) VALUES (?, ?, ?)", &sqlitex.ExecOptions{
		Args: []any{slug, url, typ},
	})
}

func (a *app) deleteSlug(slug string) error {
	a.write.Lock()
	defer a.write.Unlock()
	conn := a.dbpool.Get(context.Background())
	defer a.dbpool.Put(conn)
	return sqlitex.Execute(conn, "DELETE FROM redirect WHERE slug = ?", &sqlitex.ExecOptions{
		Args: []any{slug},
	})
}

func (a *app) updateSlug(ctx context.Context, url, typeStr, slug string) error {
	a.write.Lock()
	defer a.write.Unlock()
	conn := a.dbpool.Get(ctx)
	defer a.dbpool.Put(conn)
	return sqlitex.Execute(conn, "UPDATE redirect SET url = ?, type = ? WHERE slug = ?", &sqlitex.ExecOptions{
		Args: []any{url, typeStr, slug},
	})
}

func (a *app) increaseHits(slug string) {
	go func() {
		a.write.Lock()
		defer a.write.Unlock()
		conn := a.dbpool.Get(context.Background())
		defer a.dbpool.Put(conn)
		_ = sqlitex.Execute(conn, "UPDATE redirect SET hits = hits + 1 WHERE slug = ?", &sqlitex.ExecOptions{
			Args: []any{slug},
		})
	}()
}

func (a *app) slugExists(slug string) (exists bool, err error) {
	conn := a.dbpool.Get(context.Background())
	defer a.dbpool.Put(conn)
	err = sqlitex.Execute(conn, "SELECT EXISTS(SELECT 1 FROM redirect WHERE slug = ?)", &sqlitex.ExecOptions{
		Args: []any{slug},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			exists = stmt.ColumnInt(0) == 1
			return nil
		},
	})
	return
}
