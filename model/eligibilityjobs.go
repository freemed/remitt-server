package model

import (
	"time"
)

type EligibilityJobsModel struct {
	Id           int64     `db:"id"`
	User         string    `db:"user"`
	Inserted     time.Time `db:"inserted"`
	Processed    NullTime  `db:"processed"`
	Plugin       string    `db:"plugin"`
	Payload      []byte    `db:"payload"`
	Response     []byte    `db:"response"`
	Resubmission bool      `db:"resubmission"`
	Completed    bool      `db:"completed"`
}
