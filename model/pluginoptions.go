package model

import ()

const (
	TABLE_PLUGIN_OPTIONS = "tPluginOptions"
)

type PluginOptionsModel struct {
	PluginOption string     `db:"poption"`
	Plugin       string     `db:"plugin"`
	FullName     string     `db:"fullname"`
	Version      string     `db:"version"`
	Author       string     `db:"author"`
	Category     string     `db:"category"`
	InputFormat  NullString `db:"inputFormat"`
	OutputFormat NullString `db:"outputFormat"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_PLUGIN_OPTIONS, Obj: PluginOptionsModel{}, Key: ""})
}
