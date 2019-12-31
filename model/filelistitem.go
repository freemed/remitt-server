package model

import (
	"time"
)

type FileListItem struct {
	FileName   string    `json:"filename"`
	FileSize   int64     `json:"filesize"`
	Inserted   time.Time `json:"inserted"`
	OriginalID string    `json:"originalId"`
}
