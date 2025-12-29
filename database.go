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
	a.dbpool, err = sqlitex.NewPool(a.config.DBPath, sqlitex.PoolOptions{
		Flags:    sqlite.OpenCreate | sqlite.OpenReadWrite | sqlite.OpenWAL,
		PoolSize: 10,
	})
	if err != nil {
		return err
	}
	a.shutdown.Add(func() {
		_ = a.dbpool.Close()
		log.Println("Closed database")
	})
	a.migrateDatabase()
	// start hits aggregator
	a.hitsChan = make(chan string, 1000)
	a.startHitsAggregator()
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
			`
			update redirect set url = 'https://github.com/jlelse/GoShort' where slug = 'source';
			`,
			`
			alter table redirect add column created integer;
			update redirect set created = strftime('%s','now') where created is null;
			`,
		},
	}

	conn, err := a.dbpool.Take(context.Background())
	if err != nil {
		log.Fatal(err.Error())
		return
	}
	defer a.dbpool.Put(conn)
	err = sqlitemigration.Migrate(context.Background(), conn, schema)
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
	conn, err := a.dbpool.Take(context.Background())
	if err != nil {
		return err
	}
	defer a.dbpool.Put(conn)
	return sqlitex.ExecuteTransient(conn, "INSERT INTO redirect (slug, url, type, created) VALUES (?, ?, ?, strftime('%s','now'))", &sqlitex.ExecOptions{
		Args: []any{slug, url, typ},
	})
}

func (a *app) deleteSlug(slug string) error {
	a.write.Lock()
	defer a.write.Unlock()
	conn, err := a.dbpool.Take(context.Background())
	if err != nil {
		return err
	}
	defer a.dbpool.Put(conn)
	return sqlitex.ExecuteTransient(conn, "DELETE FROM redirect WHERE slug = ?", &sqlitex.ExecOptions{
		Args: []any{slug},
	})
}

func (a *app) updateSlug(ctx context.Context, url, typeStr, slug string) error {
	a.write.Lock()
	defer a.write.Unlock()
	conn, err := a.dbpool.Take(ctx)
	if err != nil {
		return err
	}
	defer a.dbpool.Put(conn)
	return sqlitex.ExecuteTransient(conn, "UPDATE redirect SET url = ?, type = ? WHERE slug = ?", &sqlitex.ExecOptions{
		Args: []any{url, typeStr, slug},
	})
}

func (a *app) increaseHits(slug string) {
	// Try to enqueue; if buffer is full, fall back to an asynchronous DB update so we don't drop hits.
	select {
	case a.hitsChan <- slug:
		return
	default:
		// Fallback: update DB in a goroutine (avoid blocking request handling). This ensures we don't drop hits.
		go func(s string) {
			a.write.Lock()
			defer a.write.Unlock()
			conn, err := a.dbpool.Take(context.Background())
			if err != nil {
				return
			}
			defer a.dbpool.Put(conn)
			_ = sqlitex.Execute(conn, "UPDATE redirect SET hits = hits + 1 WHERE slug = ?", &sqlitex.ExecOptions{Args: []any{s}})
		}(slug)
	}
}

func (a *app) slugExists(slug string) (exists bool, err error) {
	conn, err := a.dbpool.Take(context.Background())
	if err != nil {
		return false, err
	}
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
