package render

import "context"

// Renderer is the interface for render plugins.
type Renderer interface {
	// Render transforms input bytes into output bytes using the given option.
	Render(input []byte, option string) ([]byte, error)

	// SetContext sets the execution context.
	SetContext(context.Context) error
}
