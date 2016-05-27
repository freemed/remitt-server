package model

import ()

const (
	TABLE_JOBS = "tJobs"
)

type JobsModel struct {
	Id          int64  `db:"id"`
	JobSchedule string `db:"jobSchedule"`
	JobClass    string `db:"jobClass"`
	JobEnabled  bool   `db:"jobEnabled"`
}

func init() {
	DbTables = append(DbTables, DbTable{TableName: TABLE_JOBS, Obj: JobsModel{}, Key: "Id"})
}
