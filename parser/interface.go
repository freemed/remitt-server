package parser

import "context"

// Parser defines the interface for data parsing plugins.
type Parser interface {
	ParseData(data string) (string, error)
	SetContext(context.Context) error
}
