package transport

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
)

func init() {
	RegisterTransporter("storefilepdf", func() Transporter { return &StoreFilePdf{} })
	// The Java class is org.remitt.plugin.transport.StoreFilePdf (there is no
	// ".StoreFilePdfTransport"), see migrations/001_legacy.up.sql:227 and
	// ../remitt/src/main/java/org/remitt/plugin/transport/StoreFilePdf.java.
	registerJavaTransporter("StoreFilePdf", func() Transporter { return &StoreFilePdf{} })
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

	// A process whose database was never initialised (or whose connection
	// failed) must fail the job, not panic the worker. The guard sits directly
	// on the use of the package-level model.Queries below.
	if model.Queries == nil {
		return fmt.Errorf("storefilepdf: database is not initialised")
	}

	// The two ids tFileStore requires (both are NOT NULL foreign keys and no
	// parent row can have id 0): the payload this job is processing and the
	// stage row it was journaled under. StoreFilePdf.java:87 passes its jobId to
	// DbFileStore.putFile, which resolves the payload through it - the Java
	// plugin interface carried the job identity, and the Go Transporter
	// interface does not, so it arrives in the context
	// (common/jobcontext.go). A job without an identity fails here, with the
	// reason, instead of being rejected by the foreign key with error 1452.
	job, ok := common.JobIdentityFromContext(s.ctx)
	if !ok {
		return fmt.Errorf("storefilepdf: no job identity in context: tFileStore.payloadId references tPayload(id) and tFileStore.processorId references tProcessor(id), so the job's payload and processor ids must be attached with common.NewJobContext")
	}
	if job.PayloadID == 0 {
		return fmt.Errorf("storefilepdf: job identity carries no payload id: tFileStore.payloadId references tPayload(id) and no payload row can have id 0")
	}
	if job.ProcessorID == 0 {
		return fmt.Errorf("storefilepdf: job identity carries no processor id: tFileStore.processorId references tProcessor(id) and no processor row can have id 0")
	}

	params := dbgen.InsertFileStoreParams{
		User:     um.Username,
		Stamp:    time.Now(),
		Category: fileStoreCategory,
		// The Java original names the file itself:
		// StoreFilePdf.java:83-87 builds "<System.currentTimeMillis()>.pdf" and
		// hands that name to DbFileStore.putFile. Here the caller names the
		// file (jobqueue builds "<nano>.<ext>" from InputFormat(),
		// jobqueue.go:351-364), so the plugin enforces the same guarantee on
		// the caller's name instead of trusting it.
		Filename:    s.storedFilename(filename),
		PayloadID:   job.PayloadID,
		ProcessorID: job.ProcessorID,
		Content:     sql.NullString{String: string(payload), Valid: true},
		Contentsize: int64(len(payload)),
	}
	if _, err := model.Queries.InsertFileStore(context.Background(), params); err != nil {
		// The identity is included in the error: a rejected insert (the
		// foreign key, or a duplicate (user, category, filename)) is otherwise
		// impossible to diagnose from the worker's log.
		return fmt.Errorf("storefilepdf: insert tFileStore (user=%s category=%s filename=%s payloadId=%d processorId=%d): %w",
			um.Username, params.Category, params.Filename, job.PayloadID, job.ProcessorID, err)
	}
	return nil
}

// fileStoreCategory is the tFileStore category every row from this plugin
// carries. Both Java originals pass the literal "output" to
// DbFileStore.putFile (StoreFile.java:100, StoreFilePdf.java:87) - neither
// one derives it - and the value is part of the store's unique key
// (user, category, filename, migrations/001_legacy.up.sql:372), so it must
// stay byte-identical to the Java.
const fileStoreCategory = "output"

// storedFilename returns the name the payload is persisted under, satisfying
// the Java original's guarantee that a StoreFilePdf output is always a
// ".pdf": the extension comes from this plugin's own input format, exactly as
// StoreFilePdf.java hardcodes ".pdf" (it does NOT sniff the payload the way
// StoreFile.java:85-97 does for its pdf/xml/x12/txt cases). A caller-supplied
// name that already carries that extension is stored verbatim - jobqueue's
// "<nano>.pdf" therefore lands unchanged - and one that does not gets it
// appended rather than being persisted under a name that misdescribes the
// plugin's declared format.
func (s *StoreFilePdf) storedFilename(filename string) string {
	ext := "." + s.InputFormat()
	if strings.EqualFold(filepath.Ext(filename), ext) {
		return filename
	}
	return filename + ext
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
