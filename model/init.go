package model

import (
	"database/sql"

	"github.com/freemed/remitt-server/internal/dbgen"
)

var (
	SqlDb   *sql.DB
	Queries *dbgen.Queries
)
