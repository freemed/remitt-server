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
// "|" instead of "*". The trailing segments are still valid X12 and are
// separated by real newlines (the script trims each segment,
// 004010X098A1.js:14): with the ISA separator wrong, the separator-aware
// structure is broken, but the segment-tag-prefix checks still pass, so the
// script reports the payload as valid.
const wrongSeparatorPayload = "ISA|00|x|00|y|ZZ|REMITT|ZZ|RECEIVER|240810|1200|U|00401|000000001|0|P|:~\n" +
	"GS*HC*A*B*20240810*1200*1*X*004010X098A1~\nST*835*0001~\nSE*1*0001~\nGE*1*1~\nIEA*1*000000001~\n"

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

// Retargeted (was TestX12Validator_KnownBug_WrongElementSeparatorAccepted).
//
// The outer status is no longer hard-coded, but this payload is still reported
// as valid, because the gap is in the script's coverage rather than in the
// status rollup: the spec script only checks segment-tag prefixes after
// splitting on "~"/"\n" (resources/scripts/validation/004010X098A1.js:8-21), so
// an ISA whose element separator is "|" still counts as an ISA segment. The
// Java never reaches that point for such a payload because it derives the
// document type through the pb.x12 Parser first (X12Validator.java:56,139-145);
// enforcing delimiter integrity in the Go port is a separate change (see the
// task report). This test pins the current behaviour and fails loudly - it does
// not skip - if either the legacy "success" literal returns or the gap closes
// without the report being updated.
func TestX12Validator_WrongElementSeparator_ScriptCoverageGap(t *testing.T) {
	// Fixture sanity: the ISA element separator really is "|", the remaining
	// segments really are newline-separated, and no doubled backslash-newline
	// has crept in (that would make the script report missing segments for a
	// reason unrelated to delimiter integrity).
	if !strings.HasPrefix(wrongSeparatorPayload, "ISA|") {
		t.Fatalf("fixture must use \"|\" as the ISA element separator: %q", wrongSeparatorPayload)
	}
	if strings.Contains(wrongSeparatorPayload, `\n`) {
		t.Fatalf("fixture contains a literal backslash-n instead of a newline: %q", wrongSeparatorPayload)
	}
	if !strings.Contains(wrongSeparatorPayload, "\nGS*HC*A*B*") {
		t.Fatalf("fixture must newline-separate the trailing segments: %q", wrongSeparatorPayload)
	}

	resp := runValidator(t, []byte(wrongSeparatorPayload))

	if resp.Status != statusOK {
		t.Fatalf("behaviour changed: the wrong-element-separator payload now reports %q (%v); "+
			"delimiter integrity appears to be enforced now - update this test and the delimiter-integrity report",
			resp.Status, resp.Messages)
	}
	if want := []string{"X12 structure valid"}; !reflect.DeepEqual(resp.Messages, want) {
		t.Errorf("unexpected messages: got %v want %v", resp.Messages, want)
	}
	t.Logf("known gap: ISA element separator \"|\" reported as valid: %v", resp.Messages)
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
