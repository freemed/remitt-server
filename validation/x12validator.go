package validation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/freemed/remitt-server/config"
	"github.com/robertkrimen/otto"
	_ "github.com/robertkrimen/otto/underscore"
)

func init() {
	RegisterValidator("X12Validator", func() Validator { return &X12Validator{} })
}

// The ValidationStatus vocabulary of the Java contract
// (org.remitt.prototype.ValidationStatus, ValidationStatus.java:28). These are
// the only statuses this validator may report; the legacy Go literals
// "success" / "failure" / "error" are not part of that vocabulary.
const (
	statusOK          = "OK"
	statusWarning     = "WARNING"
	statusError       = "ERROR"
	statusServerError = "SERVER_ERROR"
)

// x12MessageSeverity mirrors the Java ValidationMessageType enum
// (ValidationMessageType.java:27-29).
type x12MessageSeverity int

const (
	severityInfo x12MessageSeverity = iota
	severityWarning
	severityError
	severityServerError
)

// scriptStatusSeverity maps the status token emitted by the embedded JavaScript
// validators (resources/scripts/validation/*.js) onto the severity the Java
// rolls the response status up from (X12Validator.java:95-115):
//
//   - "success" (004010X098A1.js:35): the script evaluated the interchange and
//     found nothing wrong -> INFO, leaves the status at OK.
//   - "failure" (004010X098A1.js:24-32): the script evaluated the interchange
//     and rejected it, naming the problems -> ERROR.
//   - "error" (004010X098A1.js:4-6): the script could not reach a verdict at
//     all (no input data) -> SERVER_ERROR, matching the Java, which reports
//     SERVER_ERROR whenever the validator itself cannot produce a verdict
//     (X12Validator.java:123-133).
//
// "warning" is part of the Java message vocabulary (ValidationMessageType.java:
// 27-29) and maps to WARNING for a future script that reports non-fatal
// findings; the checked-in scripts do not emit it today.
//
// Anything else - an unknown token, or no token at all - is SERVER_ERROR: an
// uninterpretable verdict must never be reported as OK.
func scriptStatusSeverity(status string) x12MessageSeverity {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success":
		return severityInfo
	case "failure":
		return severityError
	case "warning":
		return severityWarning
	case "error":
		return severityServerError
	default:
		return severityServerError
	}
}

// rollupStatus is the Java status rollup (X12Validator.java:100-115): the status
// starts at OK and a message may only raise it, so a later lower-severity
// message never downgrades the result.
func rollupStatus(severities []x12MessageSeverity) string {
	status := statusOK
	for _, s := range severities {
		switch s {
		case severityServerError:
			status = statusServerError
		case severityError:
			if status != statusServerError {
				status = statusError
			}
		case severityWarning:
			if status != statusServerError && status != statusError {
				status = statusWarning
			}
		case severityInfo:
			// INFO never changes the status.
		}
	}
	return status
}

// scriptResult is the verdict document the embedded scripts return from
// validate(): resources/scripts/validation/004010X098A1.js:5,32,35.
type scriptResult struct {
	Status   string   `json:"status"`
	Messages []string `json:"messages"`
}

// scriptVerdictStatus turns the script's own verdict into the Java status. The
// scripts classify the run as a whole, so every message they report carries the
// verdict's severity; the severities are then rolled up with the Java
// precedence (X12Validator.java:100-115).
func scriptVerdictStatus(res scriptResult) string {
	severity := scriptStatusSeverity(res.Status)
	n := len(res.Messages)
	if n == 0 {
		n = 1
	}
	severities := make([]x12MessageSeverity, n)
	for i := range severities {
		severities[i] = severity
	}
	return rollupStatus(severities)
}

// serverErrorResponse builds the SERVER_ERROR response used whenever the
// validator itself cannot run or cannot produce a verdict. The Java does the
// same for a script exception: SERVER_ERROR plus a SERVER_ERROR message
// (X12Validator.java:123-133).
func serverErrorResponse(message string) *ValidationResponse {
	return &ValidationResponse{
		Status:   statusServerError,
		Messages: []string{message},
	}
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

// Validate runs the X12 JavaScript validator against the provided data and
// reports the status the script's own verdict rolls up to.
func (v *X12Validator) Validate(data []byte) (*ValidationResponse, error) {
	// No configuration loaded: report a server-side failure instead of
	// dereferencing a nil config.Config.
	if config.Config == nil {
		return serverErrorResponse("No configuration loaded: config.Config is nil"), nil
	}

	basePath := config.Config.Paths.BasePath
	scriptsDir := filepath.Join(basePath, "resources", "scripts", "validation")

	vm := otto.New()

	// Expose the data to JS
	vm.Set("inputData", string(data))

	// Expose a simple log function
	vm.Set("log", func(call otto.FunctionCall) otto.Value {
		return otto.Value{}
	})

	// Run Common.js
	commonPath := filepath.Join(scriptsDir, "Common.js")
	commonScript, err := os.ReadFile(commonPath)
	if err != nil {
		return serverErrorResponse("Common.js not found: " + err.Error()), nil
	}
	if _, err := vm.Run(string(commonScript)); err != nil {
		return serverErrorResponse("Common.js execution error: " + err.Error()), nil
	}

	// Run spec validator (if exists)
	specPath := filepath.Join(scriptsDir, "004010X098A1.js")
	if specScript, err := os.ReadFile(specPath); err == nil {
		if _, err := vm.Run(string(specScript)); err != nil {
			return serverErrorResponse("Spec script error: " + err.Error()), nil
		}
	}

	// Call validate() function if it exists
	val, err := vm.Run("typeof validate === 'function' ? validate() : 'No validate() function found'")
	if err != nil {
		return serverErrorResponse("Validator script error: " + err.Error()), nil
	}

	result, err := val.ToString()
	if err != nil {
		return serverErrorResponse("Validator returned a non-string result: " + err.Error()), nil
	}

	// The script returns its verdict as a JSON document; without it there is no
	// verdict to report, so never fall back to a success status.
	var res scriptResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		return serverErrorResponse("Unparseable validator result: " + result), nil
	}

	messages := res.Messages
	if messages == nil {
		messages = []string{}
	}

	return &ValidationResponse{
		Status:   scriptVerdictStatus(res),
		Messages: messages,
	}, nil
}
