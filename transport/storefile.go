package transport

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
)

func init() {
	RegisterTransporter("storefile", func() Transporter { return &StoreFile{} })
}

type StoreFile struct {
	ctx context.Context
}

func (s *StoreFile) Transport(filename string, data any) error {
	um, ok := user.FromContext(s.ctx)
	if !ok {
		return fmt.Errorf("storefile: unable to retrieve user from context")
	}

	var payload []byte
	switch d := data.(type) {
	case string:
		payload = []byte(d)
	case []byte:
		payload = d
	default:
		return fmt.Errorf("storefile: invalid data type %T", data)
	}

	params := dbgen.InsertFileStoreParams{
		User:        um.Username,
		Stamp:       time.Now(),
		Category:    "output",
		Filename:    filename,
		PayloadID:   0,
		ProcessorID: 0,
		Content:     sql.NullString{String: string(payload), Valid: true},
		Contentsize: int64(len(payload)),
	}
	_, err := model.Queries.InsertFileStore(context.Background(), params)
	return err
}

func (s *StoreFile) InputFormat() string {
	return "*"
}

func (s *StoreFile) Options() []string {
	return []string{}
}

func (s *StoreFile) SetOptions(o map[string]any) error {
	return nil
}

func (s *StoreFile) SetContext(c context.Context) error {
	s.ctx = c
	return nil
}
