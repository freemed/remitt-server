package model

import ()

type TranslationModel struct {
	Plugin       string     `db:"plugin"`
	InputFormat  NullString `db:"inputFormat"`
	OutputFormat NullString `db:"outputFormat"`
}
