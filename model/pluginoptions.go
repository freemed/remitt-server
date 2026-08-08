package model

import (
	"context"
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

func GetPluginOptions(plugin string) ([]PluginOptionsModel, error) {
	rows, err := Queries.GetPluginOptions(context.Background(), plugin)
	if err != nil {
		return nil, err
	}
	o := make([]PluginOptionsModel, len(rows))
	for i, r := range rows {
		o[i] = PluginOptionsModel{
			PluginOption: r.Poption,
			Plugin:       r.Plugin,
			FullName:     r.Fullname,
			Version:      r.Version,
			Author:       r.Author,
			Category:     r.Category,
			InputFormat:  nullStringFromSQL(r.Inputformat),
			OutputFormat: nullStringFromSQL(r.Outputformat),
		}
	}
	return o, nil
}
