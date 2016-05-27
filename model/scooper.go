package model

import (
	"time"
)

const (
	TABLE_SCOOPER = "tScooper"
)

type ScooperModel struct {
	Id           int64     `db:"id"`
	ScooperClass string    `db:"scooperClass"`
	User         string    `db:"user"`
	Stamp        time.Time `db:"stamp"`
	Host         string    `db:"host"`
	Path         string    `db:"path"`
	Filename     string    `db:"filename"`
	Content      []byte    `db:"content"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_SCOOPER, Obj: ScooperModel{}, Key: "Id"})
}
