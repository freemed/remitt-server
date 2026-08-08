package model

import (
	"time"
)

type FileStoreModel struct {
	Id          int64     `db:"id"`
	User        string    `db:"user"`
	Stamp       time.Time `db:"stamp"`
	Category    string    `db:"category"`
	Filename    string    `db:"filename"`
	PayloadId   int64     `db:"payloadId"`
	ProcessorId int64     `db:"processorId"`
	Content     []byte    `db:"content"`
	ContentSize int64     `db:"contentSize"`
}
