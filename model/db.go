package model

import (
	"database/sql"
	"log"

	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/internal/dbgen"
	_ "github.com/go-sql-driver/mysql"
	"github.com/mattes/migrate"
	"github.com/mattes/migrate/database/mysql"
	// Required for the "file://" migration source below. Without it migrate
	// fails with "source driver: unknown driver file (forgotton import?)",
	// which is why migrations had never been able to run at all.
	_ "github.com/mattes/migrate/source/file"
)

var (
	DbFlags = "parseTime=true&multiStatements=true"
)

// dataSourceName builds the MySQL DSN from the configuration.
//
// Database.Host carries the driver's "network(addr)" form - the sample config
// uses tcp(127.0.0.1:3306) - and it was previously ignored entirely, so the
// database host could not be configured and every deployment was pinned to the
// driver's default address. An empty Host keeps that old behaviour exactly.
func dataSourceName() string {
	host := config.Config.Database.Host
	return config.Config.Database.User + ":" + config.Config.Database.Pass +
		"@" + host + "/" + config.Config.Database.Name + "?" + DbFlags
}

func InitDb() {
	dbobj, err := sql.Open("mysql", dataSourceName())
	if err != nil {
		log.Fatalln("initDb: Fail to create database", err)
	}

	// Execute migrations. The error used to be discarded entirely, so a
	// migration that never ran looked identical to one that succeeded: the
	// server started against a schema it had not created. A failure here is
	// fatal for the same reason sql.Open's is - nothing below can work without
	// the schema. "No change" is the normal case when the database is current.
	if err := MigrateDb(dbobj); err != nil && err != migrate.ErrNoChange {
		log.Fatalln("initDb: database migration failed:", err)
	}

	// Set up sqlc pool
	SqlDb = dbobj
	Queries = dbgen.New(SqlDb)
}

func MigrateDb(dbobj *sql.DB) error {
	migrationsPath := config.Config.Paths.BasePath + "/" + config.Config.Paths.DbMigrationsPath
	log.Printf("MigrateDb(): Using migrationsPath: %s", migrationsPath)
	driver, err := mysql.WithInstance(dbobj, &mysql.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithDatabaseInstance(
		"file://"+migrationsPath,
		"mysql",
		driver,
	)
	if err != nil {
		return err
	}
	err = m.Up()
	return err
}
