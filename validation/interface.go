package validation

import "context"

// ValidationResponse holds the result of a validation run.
type ValidationResponse struct {
	Status   string   `json:"status"`
	Messages []string `json:"messages"`
}

// Validator is the interface for validation plugins.
type Validator interface {
	Validate(data []byte) (*ValidationResponse, error)
	SetContext(context.Context) error
}
