package validation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/config"
)

// ---------------------------------------------------------------------------
// Test fixtures and helpers
// ---------------------------------------------------------------------------
//
// The validator resolves its JavaScript from
//   config.Config.Paths.BasePath + "resources/scripts/validation"
// (see validation/x12validator.go), so every test must set BasePath to the
// repository root. BasePath is derived from this source file's location, which
// keeps the suite independent of the process working directory.
//
// Fixture policy: only git-tracked, non-git-crypt-encrypted files are used
// (.gitattributes encrypts test/*.xml; test/testdata/** is plaintext).
// test/out.x12 is NOT tracked (gitignored, .gitignore: "test/*.x12") and is
// regenerated at runtime by translation/x12xml_test.go, so it is deliberately
// not referenced here.
//
// Response shape: ValidationResponse.Status is the Java ValidationStatus
// (OK/WARNING/ERROR/SERVER_ERROR) rolled up from the script's own verdict
// (X12Validator.java:100-115), and ValidationResponse.Messages holds the
// script's individual messages. The script's raw JSON verdict document is NOT
// returned to callers any more, so these tests observe the status directly.

// sample835Fixture is the tracked plaintext X12 835 envelope fixture.
const sample835Fixture = "../test/testdata/sample_835.x12"

// inline835 is a self-contained 835 envelope with every segment the validation
// script looks for (ISA/GS/ST/SE/GE/IEA).
const inline835 = `ISA*00*          *00*          *ZZ*REMITT          *ZZ*RECEIVER        *240810*1200*U*00401*000000001*0*P*:~
GS*HC*REMITT*RECEIVER*20240810*1200*1*X*004010X098A1~
ST*835*0001~
BPR*I*1250.00*C*CHK**01*123456789*DA*987654321*1234567890*01*20240810~
TRN*1*TRACE-001*1*987654321~
SE*4*0001~
GE*1*1~
IEA*1*000000001~
`

// truncatedEnvelope is inline835 cut off mid-envelope: ISA/GS/ST present but the
// SE/GE/IEA trailers are missing.
const truncatedEnvelope = `ISA*00*          *00*          *ZZ*REMITT          *ZZ*RECEIVER        *240810*1200*U*00401*000000001*0*P*:~
GS*HC*REMITT*RECEIVER*20240810*1200*1*X*004010X098A1~
ST*835*0001~
BPR*I*1250.00*C*CHK**01*123456789*DA*987654321*1234567890*01*20240810~
`

// wrongSeparatorPayload is a malformed envelope whose ISA element separator is
// "|" instead of "*": the header declares "|" and the segments that follow use
// "*". The trailing segments are still valid X12 and are separated by real
// newlines, and their tags still start their segments, so the tag-prefix
// presence checks in the specification script (004010X098A1.js:13-21) pass and
// used to report the payload as valid. The interchange header's own declarations
// are now enforced before the script runs (x12validator.go,
// x12StructuralFindings), so this payload is rejected.
const wrongSeparatorPayload = "ISA|00|x|00|y|ZZ|REMITT|ZZ|RECEIVER|240810|1200|U|00401|000000001|0|P|:~\n" +
	"GS*HC*A*B*20240810*1200*1*X*004010X098A1~\nST*835*0001~\nSE*1*0001~\nGE*1*1~\nIEA*1*000000001~\n"

// isaHeader108 is the well-formed interchange header the delimiter cases share:
// 16 elements, ISA16 ":" at index 106 and the declared segment terminator "~" at
// index 107. x12StructuralFindings derives both from the payload rather than
// assuming an offset, because the repo's fixtures use this 108-byte header and
// not the 106-byte header of the standard.
const isaHeader108 = "ISA*00*          *00*          *ZZ*REMITT          *ZZ*RECEIVER        *240810*1200*U*00401*000000001*0*P*:~"

// bodyHonoursHeader is an envelope body that uses the "*" element separator and
// the "~" segment terminator that isaHeader108 declares.
const bodyHonoursHeader = "GS*HC*A*B*20240810*1200*1*X*004010X098A1~\nST*835*0001~\nSE*1*0001~\nGE*1*1~\nIEA*1*000000001~\n"

// newlineOnlyEnvelope has one segment per line and no "~" separators at all.
const newlineOnlyEnvelope = "ISA*00*          *00*          *ZZ*REMITT          *ZZ*RECEIVER        *240810*1200*U*00401*000000001*0*P*:\n" +
	"GS*HC*REMITT*RECEIVER*20240810*1200*1*X*004010X098A1\n" +
	"ST*835*0001\n" +
	"SE*4*0001\n" +
	"GE*1*1\n" +
	"IEA*1*000000001\n"

// javaStatuses is the ValidationStatus vocabulary of the Java contract
// (ValidationStatus.java:28). No other value may come back from Validate.
var javaStatuses = map[string]bool{
	statusOK:          true,
	statusWarning:     true,
	statusError:       true,
	statusServerError: true,
}

// repoRoot returns the repository root (the parent of this package directory).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine source file location")
	}
	root := filepath.Dir(filepath.Dir(file))
	if _, err := os.Stat(filepath.Join(root, "resources", "scripts", "validation", "Common.js")); err != nil {
		t.Fatalf("validation scripts missing under %s: %v", root, err)
	}
	return root
}

// useConfig installs a config.Config with BasePath pointing at root and
// restores whatever was there before when the test finishes.
func useConfig(t *testing.T, root string) {
	t.Helper()
	prev := config.Config
	config.Config = &config.AppConfig{}
	config.Config.Paths.BasePath = root
	t.Cleanup(func() { config.Config = prev })
}

// newValidator is the production instantiation path used by api/validation.go:41.
func newValidator(t *testing.T) Validator {
	t.Helper()
	v, err := InstantiateValidator("X12Validator")
	if err != nil {
		t.Fatalf("InstantiateValidator(\"X12Validator\"): %v", err)
	}
	if v == nil {
		t.Fatal("InstantiateValidator returned a nil Validator with no error")
	}
	return v
}

// checkResponseShape enforces the response contract every caller depends on:
// the status is a Java ValidationStatus value (never the legacy "success"
// literal) and every entry in Messages is one individual message, not the
// script's raw JSON verdict document.
func checkResponseShape(t *testing.T, resp *ValidationResponse) {
	t.Helper()
	if resp == nil {
		t.Fatal("Validate returned a nil response")
	}
	if !javaStatuses[resp.Status] {
		t.Fatalf("Status %q is not a Java ValidationStatus value (OK/WARNING/ERROR/SERVER_ERROR); messages=%v",
			resp.Status, resp.Messages)
	}
	for i, m := range resp.Messages {
		if strings.Contains(m, `"status"`) || strings.Contains(m, `"messages"`) {
			t.Errorf("Messages[%d] is the raw script JSON document instead of an individual message: %q", i, m)
		}
		var v any
		if json.Unmarshal([]byte(m), &v) == nil {
			t.Errorf("Messages[%d] is still a JSON document: %q", i, m)
		}
	}
}

// runValidator drives the production validator against data and returns the
// caller-visible response.
func runValidator(t *testing.T, data []byte) *ValidationResponse {
	t.Helper()
	useConfig(t, repoRoot(t))
	v := newValidator(t)
	if err := v.SetContext(context.Background()); err != nil {
		t.Fatalf("SetContext: %v", err)
	}
	resp, err := v.Validate(data)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	checkResponseShape(t, resp)
	return resp
}

// ---------------------------------------------------------------------------
// 1. Registration
// ---------------------------------------------------------------------------

func TestX12Validator_Registration(t *testing.T) {
	v, err := InstantiateValidator("X12Validator")
	if err != nil {
		t.Fatalf("InstantiateValidator(\"X12Validator\") returned error: %v", err)
	}
	if v == nil {
		t.Fatal("InstantiateValidator(\"X12Validator\") returned nil validator")
	}

	x, ok := v.(*X12Validator)
	if !ok {
		t.Fatalf("expected *X12Validator, got %T", v)
	}
	var _ Validator = x // compile-time interface conformance

	// The factory must hand back a fresh instance per call.
	v2, err := InstantiateValidator("X12Validator")
	if err != nil {
		t.Fatalf("second InstantiateValidator call failed: %v", err)
	}
	if v == v2 {
		t.Error("InstantiateValidator returned the same instance twice; registry must construct new validators")
	}
}

func TestX12Validator_Registration_UnknownName(t *testing.T) {
	v, err := InstantiateValidator("NopeValidator")
	if err == nil {
		t.Fatal("expected an error for an unregistered validator name")
	}
	if v != nil {
		t.Errorf("expected nil validator on error, got %T", v)
	}
	if want := "unable to locate validator NopeValidator"; err.Error() != want {
		t.Errorf("unexpected error text:\n got: %q\nwant: %q", err.Error(), want)
	}
}

func TestX12Validator_Registration_CustomFactory(t *testing.T) {
	const name = "testOnlyValidator"
	RegisterValidator(name, func() Validator { return &stubValidator{status: "stub"} })

	v, err := InstantiateValidator(name)
	if err != nil {
		t.Fatalf("InstantiateValidator(%q): %v", name, err)
	}
	if _, ok := v.(*stubValidator); !ok {
		t.Fatalf("expected *stubValidator, got %T", v)
	}
	resp, err := v.Validate(nil)
	if err != nil {
		t.Fatalf("stub Validate: %v", err)
	}
	if resp.Status != "stub" {
		t.Errorf("expected stub status to round-trip, got %q", resp.Status)
	}
}

type stubValidator struct{ status string }

func (s *stubValidator) Validate([]byte) (*ValidationResponse, error) {
	return &ValidationResponse{Status: s.status, Messages: []string{}}, nil
}

func (s *stubValidator) SetContext(context.Context) error { return nil }

// ---------------------------------------------------------------------------
// 1b. Script verdict -> Java status mapping and severity precedence
// ---------------------------------------------------------------------------

// The status the caller sees is computed from the script's own verdict, using
// the Java ValidationStatus vocabulary and the Java severity precedence
// (X12Validator.java:100-115). These are the only statuses the checked-in
// scripts (resources/scripts/validation/004010X098A1.js) can produce:
// "success" at line 35, "failure" at line 32 and "error" at line 5.
func TestX12Validator_ScriptVerdictStatusMapping(t *testing.T) {
	cases := []struct {
		scriptStatus string
		want         string
		why          string
	}{
		{scriptStatus: "success", want: statusOK, why: "script evaluated the interchange and found nothing wrong"},
		{scriptStatus: "failure", want: statusError, why: "script evaluated the interchange and rejected it"},
		{scriptStatus: "error", want: statusServerError, why: "script could not reach a verdict"},
		{scriptStatus: "SUCCESS", want: statusOK, why: "token match is case-insensitive"},
		{scriptStatus: "warning", want: statusWarning, why: "Java message vocabulary"},
		{scriptStatus: "", want: statusServerError, why: "no verdict must never read as OK"},
		{scriptStatus: "banana", want: statusServerError, why: "uninterpretable verdict must never read as OK"},
	}

	for _, tc := range cases {
		t.Run(tc.scriptStatus, func(t *testing.T) {
			got := scriptVerdictStatus(scriptResult{Status: tc.scriptStatus, Messages: []string{"a message"}})
			if got != tc.want {
				t.Errorf("script status %q -> %q, want %q (%s)", tc.scriptStatus, got, tc.want, tc.why)
			}
		})
	}
}

// A message may only raise the status: a later lower-severity message never
// downgrades it (X12Validator.java:105-113).
func TestX12Validator_StatusPrecedenceNeverDowngrades(t *testing.T) {
	cases := []struct {
		name       string
		severities []x12MessageSeverity
		want       string
	}{
		{name: "no_messages", severities: nil, want: statusOK},
		{name: "info_only", severities: []x12MessageSeverity{severityInfo}, want: statusOK},
		{name: "warning_then_info", severities: []x12MessageSeverity{severityWarning, severityInfo}, want: statusWarning},
		{name: "error_then_warning", severities: []x12MessageSeverity{severityError, severityWarning}, want: statusError},
		{name: "warning_then_error", severities: []x12MessageSeverity{severityWarning, severityError}, want: statusError},
		{name: "error_then_server_error_then_warning", severities: []x12MessageSeverity{severityError, severityServerError, severityWarning}, want: statusServerError},
		{name: "server_error_then_info", severities: []x12MessageSeverity{severityServerError, severityInfo}, want: statusServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rollupStatus(tc.severities); got != tc.want {
				t.Errorf("rollupStatus(%v) = %q, want %q", tc.severities, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4b. Context handling / production instantiation path
// ---------------------------------------------------------------------------

func TestX12Validator_SetContext(t *testing.T) {
	useConfig(t, repoRoot(t))
	v := newValidator(t)

	ctx := context.Background()
	if err := v.SetContext(ctx); err != nil {
		t.Fatalf("SetContext(context.Background()) returned error: %v", err)
	}
	if got := v.(*X12Validator).ctx; got != ctx {
		t.Errorf("SetContext did not store the context: got %v want %v", got, ctx)
	}

	// Mirrors parser/x12271_test.go, where a nil context is accepted.
	if err := v.SetContext(nil); err != nil {
		t.Errorf("SetContext(nil) should not error, got: %v", err)
	}
}

func TestX12Validator_ProductionPath_RegistryThenValidate(t *testing.T) {
	// This is exactly the call convention in api/validation.go:41-47.
	useConfig(t, repoRoot(t))

	v, err := InstantiateValidator("X12Validator")
	if err != nil {
		t.Fatalf("InstantiateValidator: %v", err)
	}

	// api/validation.go never calls SetContext, so Validate must work without it.
	resp, err := v.Validate([]byte(inline835))
	if err != nil {
		t.Fatalf("Validate (no SetContext) returned error: %v", err)
	}
	checkResponseShape(t, resp)
	if resp.Status != statusOK {
		t.Errorf("expected status %q on the production path, got %q (%v)", statusOK, resp.Status, resp.Messages)
	}
	if want := []string{"X12 structure valid"}; !reflect.DeepEqual(resp.Messages, want) {
		t.Errorf("unexpected messages: got %v want %v", resp.Messages, want)
	}

	// And it must keep working once a context has been supplied.
	if err := v.SetContext(context.Background()); err != nil {
		t.Fatalf("SetContext: %v", err)
	}
	resp2, err := v.Validate([]byte(inline835))
	if err != nil {
		t.Fatalf("Validate (with context) returned error: %v", err)
	}
	checkResponseShape(t, resp2)
	if resp2.Status != statusOK {
		t.Errorf("expected status %q after SetContext, got %q", statusOK, resp2.Status)
	}
}

// ---------------------------------------------------------------------------
// 2. Happy path (real checked-in scripts + fixtures)
// ---------------------------------------------------------------------------

func TestX12Validator_Validate_HappyPath_Inline835(t *testing.T) {
	resp := runValidator(t, []byte(inline835))

	if resp.Status != statusOK {
		t.Fatalf("expected status %q for a valid interchange, got %q with messages %v", statusOK, resp.Status, resp.Messages)
	}
	if want := []string{"X12 structure valid"}; !reflect.DeepEqual(resp.Messages, want) {
		t.Errorf("unexpected messages: got %v want %v", resp.Messages, want)
	}
	for _, m := range resp.Messages {
		if strings.HasPrefix(m, "Missing ") {
			t.Errorf("happy path reported a missing-segment message: %q", m)
		}
	}
}

func TestX12Validator_Validate_HappyPath_Tracked835Fixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "test", "testdata", "sample_835.x12"))
	if err != nil {
		t.Fatalf("read tracked fixture %s: %v", sample835Fixture, err)
	}
	if !strings.Contains(string(data), "IEA*1*000000001") {
		t.Fatalf("fixture does not look like an X12 envelope: %q", string(data))
	}

	resp := runValidator(t, data)
	if resp.Status != statusOK {
		t.Fatalf("expected status %q for the tracked 835 fixture, got %q with messages %v", statusOK, resp.Status, resp.Messages)
	}
	if want := []string{"X12 structure valid"}; !reflect.DeepEqual(resp.Messages, want) {
		t.Errorf("unexpected messages: got %v want %v", resp.Messages, want)
	}
}

// The spec limit: the scripts split segments on "~" *or* newline
// (004010X098A1.js:8), so a newline-only envelope is accepted as well.
func TestX12Validator_Validate_HappyPath_NewlineDelimited(t *testing.T) {
	resp := runValidator(t, []byte(newlineOnlyEnvelope))
	if resp.Status != statusOK {
		t.Errorf("expected newline-delimited envelope to validate as %q, got %q (%v)", statusOK, resp.Status, resp.Messages)
	}
	if want := []string{"X12 structure valid"}; !reflect.DeepEqual(resp.Messages, want) {
		t.Errorf("unexpected messages: got %v want %v", resp.Messages, want)
	}
}

// ---------------------------------------------------------------------------
// 3. Failure paths - expected messages copied from real script output
// ---------------------------------------------------------------------------

func TestX12Validator_Validate_Malformed_TruncatedEnvelope(t *testing.T) {
	resp := runValidator(t, []byte(truncatedEnvelope))

	if resp.Status != statusError {
		t.Fatalf("expected status %q for a truncated envelope, got %q with messages %v", statusError, resp.Status, resp.Messages)
	}
	want := []string{"Missing SE segment", "Missing GE segment", "Missing IEA segment"}
	if !reflect.DeepEqual(resp.Messages, want) {
		t.Errorf("unexpected messages:\n got: %v\nwant: %v", resp.Messages, want)
	}
	if len(resp.Messages) == 0 {
		t.Error("expected a non-empty message list for a malformed payload")
	}
}

func TestX12Validator_Validate_Malformed_MissingIsaSegment(t *testing.T) {
	// Valid GS/ST/SE/GE/IEA envelope but no interchange header.
	payload := "GS*HC*REMITT*RECEIVER*20240810*1200*1*X*004010X098A1~\nST*835*0001~\nSE*1*0001~\nGE*1*1~\nIEA*1*000000001~\n"

	resp := runValidator(t, []byte(payload))
	if resp.Status != statusError {
		t.Fatalf("expected status %q for a missing ISA segment, got %q (%v)", statusError, resp.Status, resp.Messages)
	}
	want := []string{"Missing ISA segment"}
	if !reflect.DeepEqual(resp.Messages, want) {
		t.Errorf("unexpected messages:\n got: %v\nwant: %v", resp.Messages, want)
	}
}

func TestX12Validator_Validate_Malformed_MissingRequiredSegments(t *testing.T) {
	// Interchange header only; every other required segment is absent.
	resp := runValidator(t, []byte("ISA*00*x~\n"))
	if resp.Status != statusError {
		t.Fatalf("expected status %q for missing required segments, got %q (%v)", statusError, resp.Status, resp.Messages)
	}
	want := []string{"Missing GS segment", "Missing ST segment", "Missing SE segment", "Missing GE segment", "Missing IEA segment"}
	if !reflect.DeepEqual(resp.Messages, want) {
		t.Errorf("unexpected messages:\n got: %v\nwant: %v", resp.Messages, want)
	}
}

// ---------------------------------------------------------------------------
// 4. Robustness: empty, garbage, non-X12 - must never be reported as valid
// ---------------------------------------------------------------------------

func TestX12Validator_Validate_Robustness(t *testing.T) {
	xmlFixture, err := os.ReadFile(filepath.Join(repoRoot(t), "test", "testdata", "x12_intermediate.xml"))
	if err != nil {
		t.Fatalf("read XML fixture: %v", err)
	}
	if !strings.Contains(string(xmlFixture), "<?xml") {
		t.Fatalf("expected XML fixture to start with an XML declaration")
	}

	allMissing := []string{"Missing ISA segment", "Missing GS segment", "Missing ST segment", "Missing SE segment", "Missing GE segment", "Missing IEA segment"}

	// wantStatus is the Java status the script's own verdict maps to: the script
	// reports "error" (it never reached a verdict) for empty input
	// (004010X098A1.js:5) and "failure" (it evaluated and rejected the
	// interchange) for everything else (004010X098A1.js:32).
	cases := []struct {
		name        string
		input       []byte
		wantStatus  string
		wantMessage []string
	}{
		{name: "empty", input: []byte(""), wantStatus: statusServerError, wantMessage: []string{"No input data"}},
		{name: "whitespace_only", input: []byte("   \t\n  "), wantStatus: statusError, wantMessage: allMissing},
		{name: "binary_garbage", input: []byte("\x00\x01\x02garbage!!! %%% \xff\xfe not an x12 file at all\r\n\r\n"), wantStatus: statusError, wantMessage: allMissing},
		{name: "wellformed_xml_not_x12", input: xmlFixture, wantStatus: statusError, wantMessage: allMissing},
		{name: "prose_mentioning_segment_names", input: []byte("Dear sir, we send IEA and GE and ST and SE and GS and ISA greetings.\n"), wantStatus: statusError, wantMessage: allMissing},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := runValidator(t, tc.input)

			if resp.Status == statusOK {
				t.Fatalf("input %q was reported as valid (%q); messages: %v", tc.name, resp.Status, resp.Messages)
			}
			if resp.Status != tc.wantStatus {
				t.Errorf("status: got %q want %q", resp.Status, tc.wantStatus)
			}
			if len(resp.Messages) == 0 {
				t.Error("expected a non-empty message list")
			}
			if !reflect.DeepEqual(resp.Messages, tc.wantMessage) {
				t.Errorf("messages:\n got: %v\nwant: %v", resp.Messages, tc.wantMessage)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. Config / script-resolution behaviour
// ---------------------------------------------------------------------------

// When the JavaScript cannot be located the validator reports a SERVER_ERROR
// response instead of panicking or claiming success: the validator itself could
// not run, which is the Java SERVER_ERROR class (X12Validator.java:123-133).
func TestX12Validator_Validate_MissingScriptsReportsError(t *testing.T) {
	useConfig(t, t.TempDir()) // no resources/scripts/validation below it

	v := newValidator(t)
	resp, err := v.Validate([]byte(inline835))
	if err != nil {
		t.Fatalf("Validate returned a Go error instead of a SERVER_ERROR response: %v", err)
	}
	if resp.Status != statusServerError {
		t.Errorf("expected status %q when scripts are missing, got %q", statusServerError, resp.Status)
	}
	if len(resp.Messages) != 1 || !strings.HasPrefix(resp.Messages[0], "Common.js not found: ") {
		t.Errorf("unexpected message list: %#v", resp.Messages)
	}
}

// ---------------------------------------------------------------------------
// 6. Formerly "known bug" cases - retargeted to assert the fixed behaviour
// ---------------------------------------------------------------------------

// FIXED: Validate used to hard-code Status "success" regardless of what the
// JavaScript validator decided, so garbage, truncated and empty payloads were
// all reported to callers (REST and the SOAP compatibility layer) as valid.
// The status is now rolled up from the script's own verdict with the Java
// vocabulary and precedence. This test fails - it does not skip - if the false
// success comes back.
func TestX12Validator_InvalidPayloadsAreNeverReportedValid(t *testing.T) {
	cases := []struct {
		name         string
		input        []byte
		wantStatus   string
		wantContains string
	}{
		{
			name:         "empty",
			input:        []byte(""),
			wantStatus:   statusServerError,
			wantContains: "No input data",
		},
		{
			name:         "binary_garbage",
			input:        []byte("\x00\x01\x02not x12 at all\xff"),
			wantStatus:   statusError,
			wantContains: "Missing ISA segment",
		},
		{
			name:         "truncated_envelope",
			input:        []byte(truncatedEnvelope),
			wantStatus:   statusError,
			wantContains: "Missing IEA segment",
		},
		{
			name:         "missing_isa",
			input:        []byte("GS*HC*A*B*20240810*1200*1*X*004010X098A1~\nIEA*1*000000001~\n"),
			wantStatus:   statusError,
			wantContains: "Missing ISA segment",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := runValidator(t, tc.input)

			if resp.Status == statusOK {
				t.Fatalf("invalid payload reported as %q (valid); messages: %v", resp.Status, resp.Messages)
			}
			if resp.Status != tc.wantStatus {
				t.Errorf("status: got %q want %q (script verdict rolled up)", resp.Status, tc.wantStatus)
			}
			if resp.Status != statusError && resp.Status != statusServerError {
				t.Errorf("status must be %q or %q for invalid input, got %q", statusError, statusServerError, resp.Status)
			}
			joined := strings.Join(resp.Messages, "; ")
			if !strings.Contains(joined, tc.wantContains) {
				t.Errorf("messages must name the real problem (%q), got: %v", tc.wantContains, resp.Messages)
			}
			if len(resp.Messages) == 0 {
				t.Error("expected a non-empty message list")
			}
		})
	}

	t.Run("valid_envelope_is_ok", func(t *testing.T) {
		resp := runValidator(t, []byte(inline835))
		if resp.Status != statusOK {
			t.Fatalf("valid envelope must report %q, got %q (%v)", statusOK, resp.Status, resp.Messages)
		}
	})
}

// Retargeted (was TestX12Validator_WrongElementSeparator_ScriptCoverageGap,
// which pinned the old behaviour: the wrong-element-separator payload came back
// as {"status":"OK","messages":["X12 structure valid"]}).
//
// FIXED: the interchange header's own declarations are enforced on the Go side
// before the specification script's verdict is trusted, in the place the Java
// 0.5.x validator does its structural work (X12Validator.java:56 parses the
// interchange through getX12DocumentType() before it selects or runs a script).
// The script's contract is unchanged - it splits on /[~\n]/ and tests
// segment-tag prefixes (004010X098A1.js:8-21), so it can only see segment
// PRESENCE; delimiter integrity is what it cannot see and what this gate adds.
//
// Each case states the behaviour the validator must show; the ones that are
// still unhandled are named as such in the case comment and in the task report
// rather than asserted as if they were caught.
func TestX12Validator_DelimiterIntegrity(t *testing.T) {
	cases := []struct {
		name string
		in   string
		// wantStatus is the Java ValidationStatus the caller must see.
		wantStatus string
		// wantContains must appear in the joined messages for a rejection;
		// wantMessages is the exact message list for an OK verdict.
		wantContains string
		wantMessages []string
		wantWhy      string
	}{
		{
			name:         "correct_delimiters",
			in:           isaHeader108 + "\n" + bodyHonoursHeader,
			wantStatus:   statusOK,
			wantMessages: []string{"X12 structure valid"},
			wantWhy:      "header declares * and ~, body uses * and ~",
		},
		{
			name:         "correct_delimiters_newline_delimited_header",
			in:           newlineOnlyEnvelope,
			wantStatus:   statusOK,
			wantMessages: []string{"X12 structure valid"},
			wantWhy:      "the header itself is terminated by the line break, so line breaks are the declared terminator",
		},
		{
			name: "wrong_element_separator_in_body_only",
			in:   isaHeader108 + "\n" + strings.NewReplacer("*", "|").Replace(bodyHonoursHeader),
			// Used to be reported OK: every tag still starts its segment, so the
			// script's presence checks pass.
			wantStatus:   statusError,
			wantContains: `element separator "*"`,
			wantWhy:      "header declares *, body uses |",
		},
		{
			name: "wrong_element_separator_declared_in_isa",
			in:   wrongSeparatorPayload, // the payload the gap test used to pin
			// Used to be reported OK for the same reason: the body is well-formed
			// X12 ("*"/"~") and only the header's declaration is wrong.
			wantStatus:   statusError,
			wantContains: `element separator "|"`,
			wantWhy:      "header declares |, body uses *",
		},
		{
			name: "wrong_terminator",
			in:   isaHeader108 + "\n" + strings.NewReplacer("~\n", "|\n").Replace(bodyHonoursHeader),
			// Used to be reported OK: the script terminates segments on ~ *or*
			// newline, so a body it can still split is still "present".
			wantStatus:   statusError,
			wantContains: `segment terminator "~"`,
			wantWhy:      "header declares ~, body terminates with |",
		},
		{
			name: "correct_body_terminated_by_newlines_only",
			in:   isaHeader108 + "\n" + strings.NewReplacer("~\n", "\n").Replace(bodyHonoursHeader),
			// Tightening: the header declares ~, so a body that relies on line
			// breaks alone is no longer accepted. The newline *dialect* is still
			// valid when the header itself is line-terminated
			// (correct_delimiters_newline_delimited_header above).
			wantStatus:   statusError,
			wantContains: `segment terminator "~"`,
			wantWhy:      "header declares ~, body has no terminator at all",
		},
		{
			name: "isa_truncated_mid_segment",
			in: "ISA*00*          *00*          *ZZ*REMITT*ZZ*RECEI\n" +
				bodyHonoursHeader,
			// Used to be reported OK: the truncated header still starts with ISA
			// and the body supplies every segment the script looks for. The
			// header cannot be decomposed, which is what the Java's parser call
			// fails on.
			wantStatus:   statusError,
			wantContains: "ISA segment is not a well-formed interchange header",
			wantWhy:      "the ISA does not end at a segment terminator after its 16 elements",
		},
		{
			name: "correct_delimiters_plus_garbage_segment",
			in: isaHeader108 + "\n" + "GS*HC*A*B*20240810*1200*1*X*004010X098A1~\nST*835*0001~\n" +
				"ZZZ*GARBAGE*1~\nSE*2*0001~\nGE*1*1~\nIEA*1*000000001~\n",
			// Documented, not claimed as caught: an extra segment that honours the
			// delimiters is invisible to both this gate and the script (the script
			// only asks that the six envelope segments are present,
			// 004010X098A1.js:13-21), so this is still OK. Segment-level
			// vocabulary, SE/ST counts and SNIP validation are the specification
			// script's business, not this gate's.
			wantStatus:   statusOK,
			wantMessages: []string{"X12 structure valid"},
			wantWhy:      "extra well-formed segment: unhandled by design, still reported OK",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := runValidator(t, []byte(tc.in))

			if resp.Status == statusOK && tc.wantStatus != statusOK {
				t.Fatalf("payload with %s reported as %q (valid); messages: %v", tc.wantWhy, resp.Status, resp.Messages)
			}
			if resp.Status != tc.wantStatus {
				t.Errorf("status: got %q want %q (%s); messages: %v", resp.Status, tc.wantStatus, tc.wantWhy, resp.Messages)
			}
			if len(resp.Messages) == 0 {
				t.Fatal("expected a non-empty message list")
			}
			if tc.wantMessages != nil {
				if !reflect.DeepEqual(resp.Messages, tc.wantMessages) {
					t.Errorf("messages:\n got: %v\nwant: %v", resp.Messages, tc.wantMessages)
				}
				return
			}
			joined := strings.Join(resp.Messages, "; ")
			if !strings.Contains(joined, tc.wantContains) {
				t.Errorf("messages must name the problem (%q), got: %v", tc.wantContains, resp.Messages)
			}
			if resp.Status != statusError {
				t.Errorf("a delimiter defect is a verdict about the document, so the status must be %q, got %q", statusError, resp.Status)
			}
		})
	}

	// The gap fixture itself: the sanity checks the old test made still hold, so
	// the case above is really exercising the delimiter defect and not, say, a
	// literal backslash-n or a body without newlines.
	t.Run("gap_fixture_is_what_it_claims", func(t *testing.T) {
		if !strings.HasPrefix(wrongSeparatorPayload, "ISA|") {
			t.Fatalf("fixture must use \"|\" as the ISA element separator: %q", wrongSeparatorPayload)
		}
		if strings.Contains(wrongSeparatorPayload, `\n`) {
			t.Fatalf("fixture contains a literal backslash-n instead of a newline: %q", wrongSeparatorPayload)
		}
		if !strings.Contains(wrongSeparatorPayload, "\nGS*HC*A*B*") {
			t.Fatalf("fixture must newline-separate the trailing segments: %q", wrongSeparatorPayload)
		}
		// The header declares 16 elements separated by "|" and terminated by "~".
		header := wrongSeparatorPayload[:strings.Index(wrongSeparatorPayload, "\n")]
		if got := strings.Count(header, "|"); got != 16 {
			t.Errorf("fixture header must declare 16 elements, found %d separators: %q", got, header)
		}
		if !strings.HasSuffix(header, ":~") {
			t.Errorf("fixture header must end with the component separator and terminator: %q", header)
		}
	})
}

// The structural gate must not take over from the specification script on
// payloads the script already rejects: a payload that does not offer an
// unambiguous interchange header is the script's business (it reports the
// missing envelope segments), and inventing a second vocabulary for those would
// change messages the API and SOAP callers already depend on.
func TestX12StructuralFindings_NonInterchangePayloadsAreLeftToScript(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "whitespace", in: "   	\n  "},
		{name: "prose_starting_with_ISA", in: "ISA is a fine tag but this is prose, not an interchange\n"},
		{name: "binary_garbage", in: "\x00\x01\x02not x12 at all\xff"},
		{name: "isa_header_too_short_to_decompose", in: "ISA*00*x~\n"},
		{name: "newline_delimited", in: newlineOnlyEnvelope},
		{name: "well_formed_header", in: isaHeader108 + "\n" + bodyHonoursHeader},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if findings := x12StructuralFindings([]byte(tc.in)); len(findings) != 0 {
				t.Errorf("payload must be left to the script, got findings: %v", findings)
			}
		})
	}
}

// And it must produce the defect for the payload that used to slip through.
func TestX12StructuralFindings_ReportsDeclaredDelimiterMismatch(t *testing.T) {
	findings := x12StructuralFindings([]byte(wrongSeparatorPayload))
	if len(findings) == 0 {
		t.Fatal("expected a structural finding for a header declaring \"|\" while the body uses \"*\"")
	}
	joined := strings.Join(findings, "; ")
	if !strings.Contains(joined, `element separator "|"`) {
		t.Errorf("findings must name the declared element separator, got: %v", findings)
	}
	// The rejection rollup puts the findings through the Java severity
	// precedence: a document defect is ERROR, never OK and never SERVER_ERROR.
	if got := rejectedResponse(findings).Status; got != statusError {
		t.Errorf("rejectedResponse status = %q, want %q", got, statusError)
	}
}

// Retargeted (was TestX12Validator_KnownBug_NilConfigPanics).
//
// FIXED: Validate used to dereference config.Config without a nil check and
// panic when no config was loaded. It now reports the misconfiguration instead:
// either an explicit Go error or a SERVER_ERROR response. This test fails - it
// does not skip - if the panic comes back.
func TestX12Validator_NilConfigIsReportedNotPanicked(t *testing.T) {
	prev := config.Config
	config.Config = nil
	t.Cleanup(func() { config.Config = prev })

	v := newValidator(t)

	// No recover(): a panic fails the test.
	resp, err := v.Validate([]byte(inline835))
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "config") {
			t.Errorf("expected the error to name the missing configuration, got: %v", err)
		}
		return
	}
	if resp == nil {
		t.Fatal("Validate returned a nil response and a nil error")
	}
	if resp.Status != statusServerError {
		t.Fatalf("expected status %q without a loaded config, got %q (%v)", statusServerError, resp.Status, resp.Messages)
	}
	if len(resp.Messages) == 0 {
		t.Error("expected a message explaining the missing configuration")
	}
	if !strings.Contains(strings.ToLower(resp.Messages[0]), "config") {
		t.Errorf("expected the message to name the missing configuration, got %q", resp.Messages[0])
	}
}
