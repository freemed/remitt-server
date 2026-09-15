package validation

import (
	"context"
	"encoding/json"
	"fmt"
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

// ---------------------------------------------------------------------------
// Interchange structure / delimiter integrity
// ---------------------------------------------------------------------------
//
// The Java 0.5.x validator checks the structure of the interchange at its entry
// point, before any specification script is chosen or run:
// X12Validator.java:56 calls getX12DocumentType(input) as the first statement of
// validate(), and that helper (:139-145) parses the payload with the pb.x12
// Parser and reads GS element 8 to pick the script. A payload the parser cannot
// decompose never reaches the script at all, whose only contract is segment
// PRESENCE (resources/scripts/validation/004010X098A1.js:8-21 splits on /[~\n]/
// and tests segment-tag prefixes).
//
// The Go port embeds no X12 parser, so the interchange header's own declarations
// are derived and enforced here instead: the ISA declares its element separator
// and its segment terminator, and nothing else in this package reads them.

// x12ElementSeparator reports whether b is usable as an X12 element separator:
// a single printable ASCII character that is not alphanumeric. X12 forbids
// alphanumerics (and whitespace) in delimiter positions, and a payload whose
// 4th byte is one cannot be read unambiguously.
func x12ElementSeparator(b byte) bool {
	if b < 0x21 || b > 0x7e {
		return false
	}
	switch {
	case b >= '0' && b <= '9', b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z':
		return false
	}
	return true
}

// x12SegmentTerminator is x12ElementSeparator plus the line breaks: a newline
// is what terminates the segments of a newline-delimited interchange, which the
// specification script accepts (004010X098A1.js:8 splits on /[~\n]/) and which
// the test suite's newline-delimited envelope relies on.
func x12SegmentTerminator(b byte) bool {
	return b == '\n' || b == '\r' || x12ElementSeparator(b)
}

// x12EnvelopeTags are the envelope segments whose delimiter usage is verified
// against the interchange header's declarations. They are exactly the segments
// the specification script looks for (004010X098A1.js:15-20).
var x12EnvelopeTags = []string{"GS", "ST", "SE", "GE", "IEA"}

// x12SegmentTag names a body segment for a message, using the tag the segment
// starts with (up to the first separator or terminator).
func x12SegmentTag(segment string, sep, term byte) string {
	end := len(segment)
	for i := 0; i < len(segment); i++ {
		if segment[i] == sep || segment[i] == term || segment[i] == '\n' || segment[i] == '\r' {
			end = i
			break
		}
	}
	tag := strings.Trim(segment[:end], " 	")
	if tag == "" {
		return "?"
	}
	if len(tag) > 3 {
		tag = tag[:3]
	}
	return tag
}

// x12StructuralFindings inspects the interchange header (ISA) of a payload and
// reports the structural defects the specification script cannot see: an ISA
// whose declared element separator or segment terminator is not honoured by the
// segments that follow it.
//
// It returns nil when the payload offers no unambiguous ISA to check - a payload
// that does not start with ISA, or whose header cannot be decomposed into 16
// elements - because those payloads are the script's business and the script
// already rejects them ("Missing ISA segment", etc.); this must not invent a
// second failure vocabulary for them.
func x12StructuralFindings(data []byte) []string {
	s := string(data)
	if !strings.HasPrefix(s, "ISA") || len(s) < 4 {
		return nil
	}

	// The ISA declares its own element separator: it is the character
	// immediately after the "ISA" tag, i.e. byte 4 of the interchange (index 3).
	sep := s[3]
	if !x12ElementSeparator(sep) {
		// Not a delimiter position that can be read as an ISA (e.g. prose that
		// merely starts with "ISA"): leave the verdict to the script.
		return nil
	}

	// The ISA declares 16 elements, and its last element (ISA16, the component
	// element separator) is a single character, so the 16th element separator
	// locates the end of the interchange header and the character after ISA16 is
	// the segment terminator. Both are derived from the payload rather than
	// assumed: the checked-in fixtures' ISA is 108 bytes (16 separators, last at
	// index 105, ISA16 ":" at 106, "~" at 107), not the 106 bytes of the
	// standard, so a hard-coded offset would misread them.
	idx := -1
	for i, seen := 3, 0; i < len(s); i++ {
		if s[i] == sep {
			seen++
			if seen == 16 {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		// Fewer than 16 element separators: nothing here can be read as a
		// 16-element interchange header. The script reports the missing
		// envelope segments for these.
		return nil
	}
	if idx+3 > len(s) {
		return []string{"ISA segment is not a well-formed interchange header: " +
			"its 16 declared elements are not followed by a component element separator and a segment terminator"}
	}
	element16, term := s[idx+1], s[idx+2]
	if !x12ElementSeparator(element16) || !x12SegmentTerminator(term) || term == sep {
		return []string{fmt.Sprintf(
			"ISA segment is not a well-formed interchange header: its 16 declared elements end at %q (byte %d) instead of before a segment terminator",
			string(element16), idx+2)}
	}

	// Everything after the header must honour the delimiters the header just
	// declared.
	rest := s[idx+3:]

	var wrongSeparator, wrongTerminator []string
	seenTag := map[string]bool{}

	// Element separator: each envelope segment must carry the declared
	// separator directly after its tag, which is exactly what the script's
	// tag-prefix test (004010X098A1.js:13-21) does not look at.
	for _, piece := range strings.FieldsFunc(rest, func(r rune) bool {
		return r == '\n' || r == '\r' || r == rune(term)
	}) {
		piece = strings.Trim(piece, " 	")
		for _, tag := range x12EnvelopeTags {
			if strings.HasPrefix(piece, tag) && !strings.HasPrefix(piece, tag+string(sep)) && !seenTag[tag] {
				seenTag[tag] = true
				wrongSeparator = append(wrongSeparator, tag)
			}
		}
	}

	// Segment terminator: unless the header itself is terminated by a line
	// break (the newline-delimited dialect), every segment must end with the
	// declared terminator.
	if term != '\n' && term != '\r' {
		for _, line := range strings.Split(rest, "\n") {
			line = strings.Trim(strings.TrimRight(line, "\r"), " 	")
			if line == "" {
				continue
			}
			if !strings.HasSuffix(line, string(term)) {
				wrongTerminator = append(wrongTerminator, x12SegmentTag(line, sep, term))
			}
		}
	}

	var findings []string
	if len(wrongSeparator) > 0 {
		findings = append(findings, fmt.Sprintf(
			"Segments do not use the ISA-declared element separator %q: %s",
			string(sep), strings.Join(wrongSeparator, ", ")))
	}
	if len(wrongTerminator) > 0 {
		findings = append(findings, fmt.Sprintf(
			"Segments are not terminated by the ISA-declared segment terminator %q: %s",
			string(term), strings.Join(wrongTerminator, ", ")))
	}
	return findings
}

// rejectedResponse turns structural findings into the Java rejection class. The
// document declared delimiters it does not honour, so the document is invalid:
// ValidationStatus.ERROR, rolled up with the same helper the script verdicts go
// through (rollupStatus, X12Validator.java:100-115) so the two paths cannot
// disagree about severity. This is a verdict about the document, not a failure
// of the validator itself, so it is never SERVER_ERROR.
func rejectedResponse(reasons []string) *ValidationResponse {
	severities := make([]x12MessageSeverity, len(reasons))
	for i := range severities {
		severities[i] = severityError
	}
	return &ValidationResponse{
		Status:   rollupStatus(severities),
		Messages: reasons,
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

	// Structural gate, in the place the Java runs it: before the specification
	// script is selected and evaluated (X12Validator.java:56, the first
	// statement of validate(), parses the interchange through
	// getX12DocumentType() at :139-145). A payload whose declared delimiters
	// are not the delimiters its segments use never reaches the script there,
	// because the pb.x12 Parser cannot decompose it.
	if findings := x12StructuralFindings(data); len(findings) > 0 {
		return rejectedResponse(findings), nil
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
