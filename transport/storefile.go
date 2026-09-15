package transport

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/freemed/remitt-server/common"
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

	// tFileStore carries two NOT NULL foreign keys - payloadId -> tPayload(id)
	// and processorId -> tProcessor(id) - and no parent row can have id 0, so
	// both ids must come from the code that journaled the job. The Java plugin
	// was handed them as its jobId (StoreFile.java:98-101 passes it to
	// DbFileStore.putFile, which resolves the payload through it); the Go
	// Transporter interface has no such argument, so they arrive in the context
	// (common/jobcontext.go). Writing 0 instead is not a fallback: the database
	// rejects the row with error 1452, so a job without an identity fails here,
	// with the reason, instead of at the foreign key.
	job, ok := common.JobIdentityFromContext(s.ctx)
	if !ok {
		return fmt.Errorf("storefile: no job identity in context: tFileStore.payloadId references tPayload(id) and tFileStore.processorId references tProcessor(id), so the job's payload and processor ids must be attached with common.NewJobContext")
	}
	if job.PayloadID == 0 {
		return fmt.Errorf("storefile: job identity carries no payload id: tFileStore.payloadId references tPayload(id) and no payload row can have id 0")
	}
	if job.ProcessorID == 0 {
		return fmt.Errorf("storefile: job identity carries no processor id: tFileStore.processorId references tProcessor(id) and no processor row can have id 0")
	}

	params := dbgen.InsertFileStoreParams{
		User:        um.Username,
		Stamp:       time.Now(),
		Category:    "output",
		Filename:    filename,
		PayloadID:   job.PayloadID,
		ProcessorID: job.ProcessorID,
		Content:     sql.NullString{String: string(payload), Valid: true},
		Contentsize: int64(len(payload)),
	}
	if _, err := model.Queries.InsertFileStore(context.Background(), params); err != nil {
		// The identity is included in the error: a rejected insert (the
		// foreign key, or a duplicate (user, category, filename)) is otherwise
		// impossible to diagnose from the worker's log.
		return fmt.Errorf("storefile: insert tFileStore (user=%s category=%s filename=%s payloadId=%d processorId=%d): %w",
			um.Username, params.Category, params.Filename, job.PayloadID, job.ProcessorID, err)
	}
	return nil
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
