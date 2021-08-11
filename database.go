package main

import (
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/lopezator/migrator"
)

func (a *app) openDatabase() (err error) {
	if a.config.DBPath == "" {
		return errors.New("empty database path")
	}
	_ = os.MkdirAll(filepath.Dir(a.config.DBPath), 0644)
	a.database, err = sql.Open("sqlite3", a.config.DBPath+"?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=100")
	if err != nil {
		return err
	}
	a.shutdown.Add(func() {
		_ = a.database.Close()
		log.Println("Closed database")
	})
	a.migrateDatabase()
	return nil
}

func (a *app) migrateDatabase() {
	a.write.Lock()
	defer a.write.Unlock()
	m, err := migrator.New(
		migrator.Migrations(
			&migrator.Migration{
				Name: "00001",
				Func: func(tx *sql.Tx) error {
					_, err := tx.Exec(`
					drop table if exists gorp_migrations;
					create table if not exists redirect(slug text not null primary key, url text not null, type text not null default 'url', hits integer default 0 not null);
					insert or replace into redirect (slug, url) values ('source', 'https://git.jlel.se/jlelse/GoShort');
					`)
					return err
				},
			},
		),
	)
	if err != nil {
		log.Fatal(err.Error())
		return
	}
	if err := m.Migrate(a.database); err != nil {
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
	_, err := a.database.Exec("INSERT INTO redirect (slug, url, type) VALUES (?, ?, ?)", slug, url, typ)
	return err
}

func (a *app) deleteSlug(slug string) error {
	a.write.Lock()
	defer a.write.Unlock()
	_, err := a.database.Exec("DELETE FROM redirect WHERE slug = ?", slug)
	return err
}

func (a *app) increaseHits(slug string) {
	go func() {
		a.write.Lock()
		defer a.write.Unlock()
		_, _ = a.database.Exec("UPDATE redirect SET hits = hits + 1 WHERE slug = ?", slug)
	}()
}

func (a *app) slugExists(slug string) (exists bool, err error) {
	err = a.database.QueryRow("SELECT EXISTS(SELECT 1 FROM redirect WHERE slug = ?)", slug).Scan(&exists)
	return
}
