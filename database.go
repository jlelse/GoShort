package main

import (
	"database/sql"
	"log"

	"github.com/lopezator/migrator"
)

func migrateDatabase() {
	dbWriteLock.Lock()
	defer dbWriteLock.Unlock()
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
	if err := m.Migrate(appDb); err != nil {
		log.Fatal(err.Error())
		return
	}
	return
}
