package model

import (
	"time"
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
