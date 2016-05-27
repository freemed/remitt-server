package model

import ()

const (
	TABLE_TRANSLATION = "tTranslation"
)

type TranslationModel struct {
	Plugin       string     `db:"plugin"`
	InputFormat  NullString `db:"inputFormat"`
	OutputFormat NullString `db:"outputFormat"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_TRANSLATION, Obj: TranslationModel{}, Key: ""})
}
