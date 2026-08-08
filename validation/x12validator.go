package validation

import (
	"context"
	"os"
	"path/filepath"

	"github.com/freemed/remitt-server/config"
	"github.com/robertkrimen/otto"
	_ "github.com/robertkrimen/otto/underscore"
)

func init() {
	RegisterValidator("X12Validator", func() Validator { return &X12Validator{} })
}

// X12Validator validates X12 EDI payloads using embedded JavaScript.
type X12Validator struct {
	ctx context.Context
}

// SetContext sets the context for the validator.
func (v *X12Validator) SetContext(ctx context.Context) error {
	v.ctx = ctx
	return nil
}

// Validate runs the X12 JavaScript validator against the provided data.
func (v *X12Validator) Validate(data []byte) (*ValidationResponse, error) {
	vm := otto.New()

	// Expose the data to JS
	vm.Set("inputData", string(data))

	// Expose a simple log function
	vm.Set("log", func(call otto.FunctionCall) otto.Value {
		return otto.Value{}
	})

	basePath := config.Config.Paths.BasePath
	scriptsDir := filepath.Join(basePath, "resources", "scripts", "validation")

	// Run Common.js
	commonPath := filepath.Join(scriptsDir, "Common.js")
	commonScript, err := os.ReadFile(commonPath)
	if err != nil {
		return &ValidationResponse{
			Status:   "error",
			Messages: []string{"Common.js not found: " + err.Error()},
		}, nil
	}
	if _, err := vm.Run(string(commonScript)); err != nil {
		return &ValidationResponse{
			Status:   "error",
			Messages: []string{"Common.js execution error: " + err.Error()},
		}, nil
	}

	// Run spec validator (if exists)
	specPath := filepath.Join(scriptsDir, "004010X098A1.js")
	if specScript, err := os.ReadFile(specPath); err == nil {
		if _, err := vm.Run(string(specScript)); err != nil {
			return &ValidationResponse{
				Status:   "error",
				Messages: []string{"Spec script error: " + err.Error()},
			}, nil
		}
	}

	// Call validate() function if it exists
	val, err := vm.Run("typeof validate === 'function' ? validate() : 'No validate() function found'")
	if err != nil {
		return nil, err
	}

	result, _ := val.ToString()

	response := &ValidationResponse{
		Status:   "success",
		Messages: []string{result},
	}
	return response, nil
}
