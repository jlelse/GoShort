package main

import (
	migrate "github.com/rubenv/sql-migrate"
	"log"
)

func migrateDatabase() {
	migrations := &migrate.MemoryMigrationSource{
		Migrations: []*migrate.Migration{
			{
				Id:   "001",
				Up:   []string{"create table redirect(slug text not null primary key,url text not null,hits integer default 0 not null);insert into redirect (slug, url) values ('source', 'https://git.jlel.se/jlelse/GoShort');"},
				Down: []string{"drop table redirect;"},
			},
			{
				Id:   "002",
				Up:   []string{"update redirect set url = 'https://git.jlel.se/jlelse/GoShort' where slug = 'source';"},
				Down: []string{},
			},
			{
				Id:   "003",
				Up:   []string{"alter table redirect add column type text not null default 'url';"},
				Down: []string{},
			},
		},
	}
	_, err := migrate.Exec(db, "sqlite3", migrations, migrate.Up)
	if err != nil {
		log.Fatal(err)
	}
}
