package model

import (
	"database/sql"
	"errors"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jbuchbinder/migrate/driver/mysql"
	"github.com/jbuchbinder/migrate/migrate"
	"github.com/freemed/remitt-server/config"
	"gopkg.in/gorp.v1"
	"log"
	"os"
)

var (
	DbTables = make([]DbTable, 0)
        DbFlags = "parseTime=true"
)

type DbTable struct {
	TableName string
	Obj       interface{}
	Key       string
}

func InitDb() *gorp.DbMap {
	dbobj, err := sql.Open("mysql", config.Config.Database.User+":"+config.Config.Database.Pass+"@/"+config.Config.Database.Name+"?" + DbFlags)
	if err != nil {
		log.Fatalln("initDb: Fail to create database", err)
	}

        // Execute migrations
        MigrateDb()

	dbmap := &gorp.DbMap{
		Db:      dbobj,
		Dialect: gorp.MySQLDialect{"InnoDB", "UTF8"},
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

func MigrateDb() error {
        dbUrl := "mysql://" + config.Config.Database.User + ":" + config.Config.Database.Pass + "@" + config.Config.Database.Host + "/" + config.Config.Database.Name + "?" + DbFlags
        migrationsPath := config.Config.Paths.BasePath + string(os.PathSeparator) + config.Config.Paths.DbMigrationsPath
        log.Printf("MigrateDb(): Using dbUrl: %s", dbUrl)
        log.Printf("MigrateDb(): Using migrationsPath: %s", migrationsPath)
        e, ok := migrate.UpSync(dbUrl, migrationsPath)
        if !ok {
                for _, x := range e {
                        log.Print(x.Error())
                }
                return errors.New("Error executing db migrations")
        }
        return nil
}

