package callback

import (
	"context"

	"github.com/freemed/remitt-server/model"
)

// JobResult contains the result of a completed job for callback notification.
type JobResult struct {
	JobID       int64  `json:"jobId"`
	PayloadID   int64  `json:"payloadId"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	OriginalID  string `json:"originalId,omitempty"`
}

// CallbackSender is the interface for notifying originating systems
// when REMITT jobs complete.
type CallbackSender interface {
	// SendResult sends a job completion notification to the originating system.
	SendResult(ctx context.Context, user *model.UserModel, result JobResult) error
}
