package model

import ()

type PluginOptionTransformModel struct {
	PluginOptionOld string `db:"poptionold"`
	PluginOption    string `db:"poption"`
	Plugin          string `db:"plugin"`
}
