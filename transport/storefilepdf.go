package transport

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
		PayloadID:   0,
		ProcessorID: 0,
		Content:     sql.NullString{String: string(payload), Valid: true},
		Contentsize: int64(len(payload)),
	}
	_, err := model.Queries.InsertFileStore(context.Background(), params)
	return err
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
