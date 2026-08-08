package scooper

import "context"

// ScooperResult holds the result of a single file scooped from a remote source.
type ScooperResult struct {
	Filename string `json:"filename"`
	Host     string `json:"host"`
	Path     string `json:"path"`
	Content  []byte `json:"content"`
}

// Scooper is the interface for scooper plugins that poll remote sources.
type Scooper interface {
	// Scoop polls the remote source and returns results for files not yet processed.
	Scoop() ([]ScooperResult, error)

	// SetParameters configures the scooper with plugin options.
	SetParameters(params map[string]string) error

	// SetUsername sets the user owning this scooper run.
	SetUsername(user string) error

	// GetEnabledConfigValue returns the config key that enables/disables this scooper.
	GetEnabledConfigValue() string

	// SetContext sets the execution context.
	SetContext(context.Context) error
}
