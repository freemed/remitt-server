package model

import (
	"context"
	"database/sql"
)

type PluginsModel struct {
	Plugin       string     `db:"plugin"`
	Version      string     `db:"version"`
	Author       string     `db:"author"`
	Category     string     `db:"category"`
	InputFormat  NullString `db:"inputFormat"`
	OutputFormat NullString `db:"outputFormat"`
}

func GetPluginsForCategory(category string) ([]PluginsModel, error) {
	rows, err := Queries.GetPluginsByCategory(context.Background(), category)
	if err != nil {
		return nil, err
	}
	o := make([]PluginsModel, len(rows))
	for i, r := range rows {
		o[i] = PluginsModel{
			Plugin:       r.Plugin,
			Version:      r.Version,
			Author:       r.Author,
			Category:     r.Category,
			InputFormat:  nullStringFromSQL(r.Inputformat),
			OutputFormat: nullStringFromSQL(r.Outputformat),
		}
	}
	return o, nil
}

// nullStringFromSQL converts sql.NullString to model.NullString.
func nullStringFromSQL(ns sql.NullString) NullString {
	if ns.Valid {
		return NewNullStringValue(ns.String)
	}
	return NullString{}
}

// nullStringToSQL converts model.NullString to sql.NullString.
func nullStringToSQL(ns NullString) sql.NullString {
	if ns.Valid {
		return sql.NullString{String: ns.String, Valid: true}
	}
	return sql.NullString{}
}
