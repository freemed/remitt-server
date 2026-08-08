package model

import ()

type JobsModel struct {
	Id          int64  `db:"id"`
	JobSchedule string `db:"jobSchedule"`
	JobClass    string `db:"jobClass"`
	JobEnabled  bool   `db:"jobEnabled"`
}
