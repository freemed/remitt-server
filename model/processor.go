package model

import ()

type ProcessorModel struct {
	Id        int64    `db:"id"`
	ThreadId  int      `db:"threadId"`
	PayloadId int64    `db:"payloadId"`
	Stage     string   `db:"stage"`
	Plugin    string   `db:"plugin"`
	Start     NullTime `db:"tsStart"`
	End       NullTime `db:"tsEnd"`
	Input     []byte   `db:"pInput"`
	Output    []byte   `db:"pOutput"`
}
