package model

import (
	"database/sql"
	"log"

	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/internal/dbgen"
	_ "github.com/go-sql-driver/mysql"
	"github.com/mattes/migrate"
	"github.com/mattes/migrate/database/mysql"
)

var (
	DbFlags = "parseTime=true&multiStatements=true"
)

func InitDb() {
	dbobj, err := sql.Open("mysql", config.Config.Database.User+":"+config.Config.Database.Pass+"@/"+config.Config.Database.Name+"?"+DbFlags)
	if err != nil {
		log.Fatalln("initDb: Fail to create database", err)
	}

	// Execute migrations
	MigrateDb(dbobj)

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
	err = m.Steps(2)
	return err
}
