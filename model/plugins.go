package model

import ()

const (
	TABLE_PLUGINS = "tPlugins"
)

type PluginsModel struct {
	Plugin       string     `db:"plugin"`
	Version      string     `db:"version"`
	Author       string     `db:"author"`
	Category     string     `db:"category"`
	InputFormat  NullString `db:"inputFormat"`
	OutputFormat NullString `db:"outputFormat"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_PLUGINS, Obj: PluginsModel{}, Key: ""})
}

func GetPluginsForCategory(category string) ([]PluginsModel, error) {
	var o []PluginsModel
	_, err := DbMap.Select(&o, "SELECT * FROM "+TABLE_PLUGINS+" WHERE category = ?", category)
	return o, err
}

