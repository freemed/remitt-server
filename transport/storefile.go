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
	// The Java class is org.remitt.plugin.transport.StoreFile (there is no
	// ".StoreFileTransport"), see migrations/001_legacy.up.sql:226 and
	// ../remitt/src/main/java/org/remitt/plugin/transport/StoreFile.java.
	registerJavaTransporter("StoreFile", func() Transporter { return &StoreFile{} })
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

	// A process whose database was never initialised (or whose connection
	// failed) must fail the job, not panic the worker. The guard sits directly
	// on the use of the package-level model.Queries below.
	if model.Queries == nil {
		return fmt.Errorf("storefile: database is not initialised")
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
