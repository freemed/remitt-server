package model

import (
	"time"
)

const (
	TABLE_ELIGIBILITY_JOBS = "tEligibilityJobs"
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

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_ELIGIBILITY_JOBS, Obj: EligibilityJobsModel{}, Key: "Id"})
}
