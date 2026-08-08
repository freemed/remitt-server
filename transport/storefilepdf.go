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
	RegisterTransporter("storefilepdf", func() Transporter { return &StoreFilePdf{} })
}

type StoreFilePdf struct {
	ctx context.Context
}

func (s *StoreFilePdf) Transport(filename string, data any) error {
	um, ok := user.FromContext(s.ctx)
	if !ok {
		return fmt.Errorf("storefilepdf: unable to retrieve user from context")
	}

	var payload []byte
	switch d := data.(type) {
	case string:
		payload = []byte(d)
	case []byte:
		payload = d
	default:
		return fmt.Errorf("storefilepdf: invalid data type %T", data)
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

func (s *StoreFilePdf) InputFormat() string {
	return "pdf"
}

func (s *StoreFilePdf) Options() []string {
	return []string{}
}

func (s *StoreFilePdf) SetOptions(o map[string]any) error {
	return nil
}

func (s *StoreFilePdf) SetContext(c context.Context) error {
	s.ctx = c
	return nil
}
