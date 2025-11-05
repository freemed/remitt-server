package model

import (
	"database/sql"
	"log"
	"os"

	"github.com/freemed/remitt-server/config"
	"github.com/go-gorp/gorp"
	_ "github.com/go-sql-driver/mysql"
	"github.com/mattes/migrate"
	"github.com/mattes/migrate/database/mysql"
)

var (
	DbTables = make([]DbTable, 0)
	DbFlags  = "parseTime=true&multiStatements=true"
)

type DbTable struct {
	TableName string
	Obj       any
	Key       string
}

func InitDb() *gorp.DbMap {
	dbobj, err := sql.Open("mysql", config.Config.Database.User+":"+config.Config.Database.Pass+"@/"+config.Config.Database.Name+"?"+DbFlags)
	if err != nil {
		log.Fatalln("initDb: Fail to create database", err)
	}

	// Execute migrations
	MigrateDb(dbobj)

	dbmap := &gorp.DbMap{
		Db:      dbobj,
		Dialect: gorp.MySQLDialect{Engine: "InnoDB", Encoding: "UTF8"},
	}

	//dbmap.AddTableWithName(MyUserModel{}, "users").SetKeys(true, "Id")
	for _, v := range DbTables {
		keyName := v.Key
		log.Printf("initDb: Adding table %s", v.TableName)
		if keyName != "" {
			dbmap.AddTableWithName(v.Obj, v.TableName).SetKeys(true, keyName)
		} else {
			dbmap.AddTableWithName(v.Obj, v.TableName)
		}
	}

	err = dbmap.CreateTablesIfNotExists()
	if err != nil {
		log.Fatalln("initDb: Could not build tables", err)
	}

	dbmap.TraceOn("[gorp]", log.New(os.Stdout, "db: ", log.Lmicroseconds))

	return dbmap
}

func MigrateDb(dbobj *sql.DB) error {
	migrationsPath := config.Config.Paths.BasePath + string(os.PathSeparator) + config.Config.Paths.DbMigrationsPath
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
