// Package common: job identity plumbing for the pipeline's plugin calls.
//
// # Why this exists
//
// The Java pipeline called every plugin with the id of the tProcessor row the
// stage was journaled under: PluginInterface.render(Integer jobId, byte[] input,
// String option), called from the stage threads (TransportProcessorThread). The
// transports that persist their output used it: StoreFile.java hands its jobId
// to DbFileStore.putFile, which resolves the payload through it
// (DbFileStore.java: `getPayloadFromProcessor(processorId).getId()`), and the
// row it writes carries BOTH ids.
//
// The Go port's Transporter interface is Transport(filename string, data any) -
// no job identity - so the two ids have to travel beside the call. They travel
// in the context the pipeline already passes (jobqueue calls
// transportPlugin.SetContext(ctx), jobqueue.go:336, exactly as it does for the
// user), which is what this file provides.
//
// # What the database requires
//
// tFileStore (migrations/001_legacy.up.sql:364-376) has two NOT NULL foreign
// keys, and neither column may hold 0 because no parent row can have that id:
//
//	payloadId   -> tPayload(id)    (tFileStore_ibfk_1)
//	processorId -> tProcessor(id)  (tFileStore_ibfk_2)
//
// The two can only be supplied by the code that journals the job: the payload
// row exists before the job starts, and the tProcessor row is written per stage
// (the Java ControlThread.migratePayloadToProcessor wrote it). A plugin can
// therefore neither invent nor derive them, which is why a plugin that is handed
// no identity must fail loudly instead of writing an invalid row.
package common

import (
	"context"
)

// JobIdentity carries the database identity of the job a plugin is running for.
//
// The zero value is "nothing known", which is reported as a MISS by
// JobIdentityFromContext when it was never attached at all.
type JobIdentity struct {
	// PayloadID is tPayload.id - the payload the job is processing. Required
	// by every table that references a payload (tFileStore.payloadId).
	PayloadID uint64

	// ProcessorID is tProcessor.id - the stage row this job was journaled
	// under, which is what the Java plugin interface called the jobId.
	// Required by every table that references a processor
	// (tFileStore.processorId).
	ProcessorID uint64

	// JobID is the in-memory queue id (jobqueue.JobQueueItem.ID). It is NOT a
	// database key and nothing may be written from it; it exists so a plugin's
	// errors and logs name the job the way the queue does.
	JobID int64
}

// key is an unexported type for keys defined in this package. This prevents
// collisions with keys defined in other packages.
type key int

// jobIdentityKey is the key for JobIdentity values in Contexts. It is
// unexported; clients use NewJobContext and JobIdentityFromContext instead of
// using this key directly.
var jobIdentityKey key = 0

// NewJobContext returns a new Context that carries the job identity. It is
// meant to be applied to the same context the user was attached to
// (user.NewContext), so a plugin can read both:
//
//	ctx = common.NewJobContext(ctx, common.JobIdentity{PayloadID: p, ProcessorID: c})
func NewJobContext(ctx context.Context, id JobIdentity) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, jobIdentityKey, id)
}

// JobIdentityFromContext returns the JobIdentity stored in ctx, if any, and
// whether one was found. A context that never had an identity attached (or that
// carries a different type under this key) is a MISS: callers get the zero value
// and false rather than a panic or a partially-populated struct.
func JobIdentityFromContext(ctx context.Context) (JobIdentity, bool) {
	if ctx == nil {
		return JobIdentity{}, false
	}
	if id, ok := ctx.Value(jobIdentityKey).(JobIdentity); ok {
		return id, true
	}
	return JobIdentity{}, false
}
