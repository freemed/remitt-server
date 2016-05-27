package model

import (
	"time"
)

const (
	TABLE_PAYLOAD         = "tPayload"
	PayloadStateValid     = "valid"
	PayloadStateFailed    = "failed"
	PayloadStateCompleted = "completed"
)

type PayloadModel struct {
	Id              int64      `db:"id"`
	InsertStamp     time.Time  `db:"insert_stamp"`
	User            string     `db:"user"`
	Payload         []byte     `db:"payload"`
	OriginalId      NullString `db:"originalId"`
	RenderPlugin    string     `db:"renderPlugin"`
	RenderOption    string     `db:"renderOption"`
	TransportPlugin string     `db:"transportPlugin"`
	TransportOption string     `db:"transportOption"`
	PayloadState    string     `db:"payloadState"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_PAYLOAD, Obj: PayloadModel{}, Key: "Id"})
}
