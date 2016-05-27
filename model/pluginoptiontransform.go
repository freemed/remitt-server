package model

import ()

const (
	TABLE_PLUGIN_OPTION_TRANSFORM = "tPluginOptionTransform"
)

type PluginOptionTransformModel struct {
	PluginOptionOld string `db:"poptionold"`
	PluginOption    string `db:"poption"`
	Plugin          string `db:"plugin"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_PLUGIN_OPTION_TRANSFORM, Obj: PluginOptionTransformModel{}, Key: ""})
}
