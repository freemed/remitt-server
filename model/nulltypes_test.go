// Package model: null* type round-tripping, JSON and database/sql wiring.
//
// # Scope
//
// NullString, NullInt64 and NullTime are the JSON boundary of this server: the
// API and SOAP layers decode client documents into them and the sqlc layer
// scans database columns into them. These tests pin that boundary without a
// database - Scan and Value are exercised through their driver.Value contract
// only, and json/xml are exercised in memory. Nothing here dials MySQL; see
// TestDatabaseBoundAPIRequiresDatabase in models_test.go for the functions that
// cannot be tested without one.
//
// # Defects documented here (pinned, not fixed)
//
//  1. NullString.UnmarshalJSON has a VALUE receiver (nullstring.go:29), so
//     unmarshalling into a *NullString - including into any struct field, which
//     is how api/payload.go:26 and api/api.go decode payloads - is a silent
//     no-op: the document always decodes to the zero value and no error is
//     reported. TestNullStringUnmarshalJSONIsANoOp.
//  2. NullString.MarshalJSON uses strconv.QuoteToASCII (nullstring.go:26), which
//     emits Go escapes. For the two characters whose Go escape is not a legal
//     JSON escape (\a U+0007 and \v U+000B) the returned bytes are rejected by
//     encoding/json, so json.Marshal of any structure containing such a
//     NullString fails outright.
//     TestNullStringMarshalJSONRejectsGoOnlyEscapes.
//  3. NullTime.UnmarshalJSON rejects the JSON literal null with a parse error
//     (nulltime.go:36-45) instead of treating it as "no value"; it also accepts
//     any JSON value shorter than 3 bytes as a silent "invalid" with no error,
//     so `""` and `1` are swallowed while `null` and `1234` are hard errors.
//     TestNullTimeUnmarshalJSONNullAndShortInputs.
//
// The NewNullStringValue defect (nullstring.go:16-20) and the conversion
// helpers that depend on it are pinned in pluginoptions_test.go.
package model

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NullString
// ---------------------------------------------------------------------------

// validNullString builds a NullString that is genuinely valid, without going
// through NewNullStringValue (whose behaviour is pinned separately).
func validNullString(s string) NullString {
	var out NullString
	out.String = s
	out.Valid = true
	return out
}

func TestNullStringMarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   NullString
		want string
	}{
		{name: "zero_value_is_null", in: NullString{}, want: "null"},
		{name: "invalid_with_text_is_null", in: NullString{NullString: sql.NullString{String: "ignored"}}, want: "null"},
		{name: "valid_empty_string_is_empty_quoted", in: validNullString(""), want: `""`},
		{name: "valid_ascii", in: validNullString("abc"), want: `"abc"`},
		{name: "valid_with_quote", in: validNullString(`a"b`), want: `"a\"b"`},
		{name: "valid_with_backslash", in: validNullString(`a\b`), want: `"a\\b"`},
		{name: "valid_with_newline", in: validNullString("a\nb"), want: `"a\nb"`},
		{name: "valid_with_tab", in: validNullString("a\tb"), want: `"a\tb"`},
		{name: "valid_with_carriage_return", in: validNullString("a\rb"), want: `"a\rb"`},
		{name: "valid_non_ascii_is_ascii_escaped", in: validNullString("héllo"), want: `"h\u00e9llo"`},
		{name: "valid_backspace", in: validNullString("\b"), want: `"\b"`},
		{name: "valid_formfeed", in: validNullString("\f"), want: `"\f"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("json.Marshal(%#v) returned %v", tc.in, err)
			}
			if string(got) != tc.want {
				t.Errorf("json.Marshal = %s, want %s", got, tc.want)
			}
			// Whatever the encoder produced must be valid JSON.
			if !json.Valid(got) {
				t.Errorf("encoder produced invalid JSON: %s", got)
			}
		})
	}
}

func TestNullStringMarshalJSONRejectsGoOnlyEscapes(t *testing.T) {
	// Documented defect: strconv.QuoteToASCII (nullstring.go:26) renders the
	// characters below with Go escapes that JSON does not define: U+0007 -> \a,
	// U+000B -> \v, and every other unprintable ASCII byte (U+0000-U+0006,
	// U+000E-U+001F and U+007F) as \xNN. The bytes MarshalJSON returns are then
	// rejected by encoding/json, so json.Marshal reports an error and produces NO
	// output at all for a value that is perfectly storable in MySQL.
	cases := []struct {
		name string
		in   string
	}{
		{name: "bell_u0007", in: "a\ab"},
		{name: "vertical_tab_u000b", in: "a\vb"},
		{name: "del_u007f", in: "a\x7fb"},
		{name: "start_of_heading_u0001", in: "a\x01b"},
		{name: "shift_out_u000e", in: "a\x0eb"},
		{name: "unit_separator_u001f", in: "a\x1fb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ns := validNullString(tc.in)

			raw, err := ns.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON returned an error for %q: %v", tc.in, err)
			}
			if json.Valid(raw) {
				t.Fatalf("fixture no longer reproduces the defect: MarshalJSON(%q) = %s is valid JSON", tc.in, raw)
			}
			if !strings.Contains(string(raw), `\`) {
				t.Fatalf("expected a Go-style escape in %s", raw)
			}

			out, err := json.Marshal(ns)
			if err == nil {
				t.Fatalf("json.Marshal unexpectedly succeeded with %s", out)
			}
			if !strings.Contains(err.Error(), "error calling MarshalJSON for type model.NullString") {
				t.Errorf("unexpected error text: %v", err)
			}

			// The failure is contagious: any structure holding the value fails.
			type wrapper struct {
				F NullString `json:"f"`
			}
			if _, err := json.Marshal(wrapper{F: ns}); err == nil {
				t.Error("expected a struct containing the value to fail as well")
			}
		})
	}
}

func TestNullStringUnmarshalJSONIsANoOp(t *testing.T) {
	// Documented defect: UnmarshalJSON is declared on the value receiver
	// (nullstring.go:29), so it edits a copy - the delegate call it makes is
	// json.Unmarshal(b, &s.String) against that copy. Two things follow, and
	// this table pins both: a document the delegate accepts (a string or null)
	// decodes silently to NOTHING, and a document it rejects returns an error
	// even though nothing could have been written anyway. In no case does the
	// destination NullString change - which is how a valid client payload can
	// silently lose its original_id.
	cases := []struct {
		name      string
		doc       string
		wantError string
	}{
		{name: "string", doc: `"hello"`},
		{name: "empty_string", doc: `""`},
		{name: "null", doc: `null`},
		{name: "number", doc: `5`, wantError: "cannot unmarshal number into Go value of type string"},
		{name: "bool", doc: `true`, wantError: "cannot unmarshal bool into Go value of type string"},
		{name: "array", doc: `[1,2]`, wantError: "cannot unmarshal array into Go value of type string"},
		{name: "object_with_fields", doc: `{"String":"hello","Valid":true}`,
			wantError: "cannot unmarshal object into Go value of type string"},
		{name: "object_lowercase_fields", doc: `{"string":"hello","valid":true}`,
			wantError: "cannot unmarshal object into Go value of type string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ns NullString
			err := json.Unmarshal([]byte(tc.doc), &ns)
			if tc.wantError != "" {
				if err == nil {
					t.Fatalf("document %s unexpectedly decoded; the value receiver was fixed - update this test", tc.doc)
				}
				if !strings.Contains(err.Error(), tc.wantError) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantError)
				}
			} else if err != nil {
				t.Fatalf("json.Unmarshal(%s) returned %v", tc.doc, err)
			}
			if ns.Valid || ns.String != "" {
				t.Fatalf("document %s populated the value (%+v); the value receiver was fixed - update this test", tc.doc, ns)
			}
		})
	}

	t.Run("through_a_struct_field", func(t *testing.T) {
		// This is the shape every API handler and client payload uses.
		type payload struct {
			OriginalID NullString `json:"original_id"`
			Body       string     `json:"input_payload"`
		}
		var p payload
		if err := json.Unmarshal([]byte(`{"original_id":"T-1","input_payload":"DATA"}`), &p); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if p.Body != "DATA" {
			t.Errorf("ordinary string field = %q, want DATA", p.Body)
		}
		if p.OriginalID.Valid || p.OriginalID.String != "" {
			t.Errorf("original_id decoded to %+v; the value-receiver defect is fixed - update this test", p.OriginalID)
		}
	})

	t.Run("an_already_valid_value_is_left_alone", func(t *testing.T) {
		ns := validNullString("kept")
		if err := json.Unmarshal([]byte(`"replaced"`), &ns); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if ns.String != "kept" || !ns.Valid {
			t.Errorf("value = %+v, want the pre-existing value untouched (no-op)", ns)
		}
	})
}

func TestNullStringSQLScanAndValue(t *testing.T) {
	// The embedded sql.NullString keeps its driver contract: a MySQL NULL scans
	// to Valid=false and a VARCHAR to Valid=true.
	t.Run("scan_null", func(t *testing.T) {
		var ns NullString
		if err := ns.Scan(nil); err != nil {
			t.Fatalf("Scan(nil): %v", err)
		}
		if ns.Valid || ns.String != "" {
			t.Errorf("Scan(nil) = %+v, want the zero value", ns)
		}
	})

	t.Run("scan_string", func(t *testing.T) {
		var ns NullString
		if err := ns.Scan("dbvalue"); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if !ns.Valid || ns.String != "dbvalue" {
			t.Errorf("Scan = %+v, want the value marked valid", ns)
		}
		b, err := json.Marshal(ns)
		if err != nil {
			t.Fatalf("json.Marshal after Scan: %v", err)
		}
		if string(b) != `"dbvalue"` {
			t.Errorf("scanned value marshals as %s, want \"dbvalue\"", b)
		}
	})

	t.Run("scan_bytes", func(t *testing.T) {
		var ns NullString
		if err := ns.Scan([]byte("bytesvalue")); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if !ns.Valid || ns.String != "bytesvalue" {
			t.Errorf("Scan = %+v", ns)
		}
	})

	t.Run("value_of_invalid_is_nil", func(t *testing.T) {
		v, err := NullString{}.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if v != nil {
			t.Errorf("Value() = %#v, want nil for an invalid value", v)
		}
	})

	t.Run("value_of_valid_is_string", func(t *testing.T) {
		v, err := validNullString("x").Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if v != "x" {
			t.Errorf("Value() = %#v, want \"x\"", v)
		}
	})
}

// ---------------------------------------------------------------------------
// NullInt64
// ---------------------------------------------------------------------------

func TestNullInt64MarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   NullInt64
		want string
	}{
		{name: "zero_value_is_null", in: NullInt64{}, want: "null"},
		{name: "invalid_ignores_int64", in: NullInt64{NullInt64: sql.NullInt64{Int64: 9}}, want: "null"},
		{name: "valid_zero_is_zero", in: NullInt64{NullInt64: sql.NullInt64{Int64: 0, Valid: true}}, want: "0"},
		{name: "valid_positive", in: NullInt64{NullInt64: sql.NullInt64{Int64: 42, Valid: true}}, want: "42"},
		{name: "valid_negative", in: NullInt64{NullInt64: sql.NullInt64{Int64: -5, Valid: true}}, want: "-5"},
		{name: "valid_min_int64", in: NullInt64{NullInt64: sql.NullInt64{Int64: math.MinInt64, Valid: true}}, want: "-9223372036854775808"},
		{name: "valid_max_int64", in: NullInt64{NullInt64: sql.NullInt64{Int64: math.MaxInt64, Valid: true}}, want: "9223372036854775807"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("json.Marshal = %s, want %s", got, tc.want)
			}
			if !json.Valid(got) {
				t.Errorf("invalid JSON emitted: %s", got)
			}
		})
	}
}

func TestNullInt64UnmarshalJSON(t *testing.T) {
	cases := []struct {
		name      string
		doc       string
		wantInt   int64
		wantValid bool
		wantErr   string
	}{
		{name: "positive", doc: `5`, wantInt: 5, wantValid: true},
		{name: "negative", doc: `-5`, wantInt: -5, wantValid: true},
		{name: "zero_is_valid", doc: `0`, wantInt: 0, wantValid: true},
		{name: "max_int64", doc: `9223372036854775807`, wantInt: math.MaxInt64, wantValid: true},
		{name: "null_clears", doc: `null`, wantInt: 0, wantValid: false},
		{name: "object_form_upper", doc: `{"Int64":7,"Valid":true}`, wantInt: 7, wantValid: true},
		{name: "object_form_lower", doc: `{"int64":7,"valid":true}`, wantInt: 7, wantValid: true},
		// Documented quirk: the object branch delegates to sql.NullInt64 and then
		// overwrites Valid with `err == nil` (nullint.go:43), so the document's
		// own Valid field is ignored - {"Int64":0,"Valid":false} would decode as
		// valid. The `{}` case shows the same: nothing in the document is
		// required for the result to claim validity.
		{name: "object_form_without_valid_field", doc: `{"Int64":7}`,
			wantInt: 7, wantValid: true},
		{name: "object_form_explicitly_invalid", doc: `{"Int64":0,"Valid":false}`,
			wantInt: 0, wantValid: true},
		{name: "empty_object", doc: `{}`, wantInt: 0, wantValid: true},
		{name: "fractional_number_rejected", doc: `5.5`,
			wantErr: "json: cannot unmarshal number 5.5 into Go value of type int64"},
		{name: "exponent_number_rejected", doc: `1e3`,
			wantErr: "json: cannot unmarshal number 1e3 into Go value of type int64"},
		{name: "out_of_range_number_rejected", doc: `9223372036854775808`,
			wantErr: "cannot unmarshal number 9223372036854775808"},
		{name: "string_rejected", doc: `"5"`,
			wantErr: "json: cannot unmarshal string into Go value of type null.Int"},
		{name: "bool_rejected", doc: `true`,
			wantErr: "json: cannot unmarshal bool into Go value of type null.Int"},
		{name: "array_rejected", doc: `[1]`,
			wantErr: "json: cannot unmarshal  into Go value of type null.Int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v NullInt64
			err := json.Unmarshal([]byte(tc.doc), &v)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got %+v", tc.wantErr, v)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				if v.Valid {
					t.Errorf("a failed unmarshal must not leave the value valid: %+v", v)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Int64 != tc.wantInt || v.Valid != tc.wantValid {
				t.Errorf("value = {%d %v}, want {%d %v}", v.Int64, v.Valid, tc.wantInt, tc.wantValid)
			}
		})
	}
}

func TestNullInt64UnmarshalJSONClearsPreviousValue(t *testing.T) {
	// UnmarshalJSON assigns i.Valid = (err == nil) on every call, so a document
	// of null resets a previously valid value.
	v := NullInt64{NullInt64: sql.NullInt64{Int64: 12, Valid: true}}
	if err := json.Unmarshal([]byte(`null`), &v); err != nil {
		t.Fatalf("json.Unmarshal(null): %v", err)
	}
	if v.Valid {
		t.Errorf("value = %+v, want Valid=false after a null document", v)
	}
}

func TestNullInt64RoundTrip(t *testing.T) {
	cases := []NullInt64{
		{},
		{NullInt64: sql.NullInt64{Int64: 0, Valid: true}},
		{NullInt64: sql.NullInt64{Int64: 7, Valid: true}},
		{NullInt64: sql.NullInt64{Int64: -7, Valid: true}},
		{NullInt64: sql.NullInt64{Int64: math.MaxInt64, Valid: true}},
	}
	for _, in := range cases {
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("json.Marshal(%+v): %v", in, err)
		}
		var out NullInt64
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("json.Unmarshal(%s): %v", b, err)
		}
		if out.Int64 != in.Int64 || out.Valid != in.Valid {
			t.Errorf("round trip of %s = {%d %v}, want {%d %v}", b, out.Int64, out.Valid, in.Int64, in.Valid)
		}
	}
}

func TestNullInt64InStruct(t *testing.T) {
	type doc struct {
		N NullInt64 `json:"n"`
	}
	cases := []struct {
		name string
		in   doc
		want string
	}{
		{name: "invalid", in: doc{}, want: `{"n":null}`},
		{name: "valid_zero", in: doc{N: NullInt64{NullInt64: sql.NullInt64{Valid: true}}}, want: `{"n":0}`},
		{name: "valid_value", in: doc{N: NullInt64{NullInt64: sql.NullInt64{Int64: 3, Valid: true}}}, want: `{"n":3}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("json.Marshal = %s, want %s", got, tc.want)
			}
			var back doc
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if back.N.Int64 != tc.in.N.Int64 || back.N.Valid != tc.in.N.Valid {
				t.Errorf("round trip = {%d %v}, want {%d %v}", back.N.Int64, back.N.Valid, tc.in.N.Int64, tc.in.N.Valid)
			}
		})
	}
}

func TestNullInt64SQLScanAndValue(t *testing.T) {
	// Scan is inherited from sql.NullInt64 (convert.go), i.e. the MySQL driver's
	// integer types all pass through.
	cases := []struct {
		name      string
		in        any
		wantInt   int64
		wantValid bool
		wantErr   bool
	}{
		{name: "nil", in: nil, wantInt: 0, wantValid: false},
		{name: "int64", in: int64(11), wantInt: 11, wantValid: true},
		{name: "bytes", in: []byte("12"), wantInt: 12, wantValid: true},
		{name: "string", in: "13", wantInt: 13, wantValid: true},
		{name: "unparseable_string", in: "nope", wantErr: true},
		{name: "unsupported_type", in: struct{}{}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v NullInt64
			err := v.Scan(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error scanning %#v, got %+v", tc.in, v)
				}
				return
			}
			if err != nil {
				t.Fatalf("Scan(%#v): %v", tc.in, err)
			}
			if v.Int64 != tc.wantInt || v.Valid != tc.wantValid {
				t.Errorf("Scan(%#v) = {%d %v}, want {%d %v}", tc.in, v.Int64, v.Valid, tc.wantInt, tc.wantValid)
			}
		})
	}

	t.Run("value", func(t *testing.T) {
		v, err := NullInt64{}.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if v != nil {
			t.Errorf("invalid Value() = %#v, want nil", v)
		}
		v, err = NullInt64{NullInt64: sql.NullInt64{Int64: 5, Valid: true}}.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if v != int64(5) {
			t.Errorf("Value() = %#v, want int64(5)", v)
		}
	})
}

// ---------------------------------------------------------------------------
// NullTime
// ---------------------------------------------------------------------------

func TestNullTimeScanAndValue(t *testing.T) {
	base := time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC)

	t.Run("scan_nil_is_invalid", func(t *testing.T) {
		var nt NullTime
		if err := nt.Scan(nil); err != nil {
			t.Fatalf("Scan(nil): %v", err)
		}
		if nt.Valid {
			t.Error("Scan(nil) must leave the value invalid")
		}
		if !nt.Time.IsZero() {
			t.Errorf("Scan(nil) left a non-zero time: %v", nt.Time)
		}
	})

	t.Run("scan_time_is_valid", func(t *testing.T) {
		var nt NullTime
		if err := nt.Scan(base); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if !nt.Valid {
			t.Fatal("Scan(time.Time) must mark the value valid")
		}
		if !nt.Time.Equal(base) {
			t.Errorf("time = %v, want %v", nt.Time, base)
		}
	})

	t.Run("scan_zero_time_is_valid", func(t *testing.T) {
		// A zero time.Time is still a time.Time, so it scans as valid; the
		// zero-ness is not treated as SQL NULL.
		var nt NullTime
		if err := nt.Scan(time.Time{}); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if !nt.Valid {
			t.Error("Scan(time.Time{}) should be valid")
		}
	})

	t.Run("scan_other_types_silently_invalidate", func(t *testing.T) {
		// Documented behaviour: Scan is a comma-ok type assertion
		// (nulltime.go:14-17), so any non-time.Time value (the string and []byte
		// forms a driver may hand back for a DATETIME column) clears the value
		// and reports SUCCESS. There is no error to notice.
		for _, in := range []any{"2024-01-02T15:04:05Z", []byte("2024-01-02 15:04:05"), int64(1704207845), 3.5} {
			var nt NullTime
			if err := nt.Scan(in); err != nil {
				t.Errorf("Scan(%#v) returned %v; the assertion is comma-ok and never errors", in, err)
			}
			if nt.Valid {
				t.Errorf("Scan(%#v) reported Valid=true", in)
			}
		}
	})

	t.Run("value", func(t *testing.T) {
		v, err := NullTime{}.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if v != nil {
			t.Errorf("invalid Value() = %#v, want nil", v)
		}
		got, err := NullTime{Time: base, Valid: true}.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		tv, ok := got.(time.Time)
		if !ok {
			t.Fatalf("Value() returned %T, want time.Time", got)
		}
		if !tv.Equal(base) {
			t.Errorf("Value() = %v, want %v", tv, base)
		}
	})

	t.Run("round_trip", func(t *testing.T) {
		var scanned NullTime
		if err := scanned.Scan(base); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		v, err := scanned.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		var again NullTime
		if err := again.Scan(v); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if !again.Valid || !again.Time.Equal(base) {
			t.Errorf("round trip = %+v, want %v", again, base)
		}
	})
}

func TestNullTimeMarshalJSON(t *testing.T) {
	base := time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC)
	cases := []struct {
		name string
		in   NullTime
		want string
	}{
		{name: "invalid_is_null", in: NullTime{}, want: "null"},
		{name: "invalid_with_time_is_null", in: NullTime{Time: base}, want: "null"},
		{name: "valid_utc", in: NullTime{Time: base, Valid: true}, want: `"2024-01-02T15:04:05Z"`},
		{name: "valid_with_nanoseconds", in: NullTime{Time: base.Add(123456789 * time.Nanosecond), Valid: true}, want: `"2024-01-02T15:04:05.123456789Z"`},
		{name: "valid_with_offset", in: NullTime{Time: time.Date(2024, 1, 2, 15, 4, 5, 0, time.FixedZone("EST", -5*3600)), Valid: true}, want: `"2024-01-02T15:04:05-05:00"`},
		{name: "valid_zero_time", in: NullTime{Time: time.Time{}, Valid: true}, want: `"0001-01-01T00:00:00Z"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("json.Marshal = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestNullTimeUnmarshalJSON(t *testing.T) {
	cases := []struct {
		name      string
		doc       string
		wantUnix  int64
		wantValid bool
		wantErr   string
	}{
		{name: "rfc3339_utc", doc: `"2024-01-02T15:04:05Z"`, wantUnix: 1704207845, wantValid: true},
		{name: "rfc3339_offset", doc: `"2024-01-02T15:04:05+05:00"`, wantUnix: 1704207845 - 5*3600, wantValid: true},
		{name: "rfc3339_fractional", doc: `"2024-01-02T15:04:05.5Z"`, wantUnix: 1704207845, wantValid: true},
		{name: "date_only_rejected", doc: `"2024-01-02"`, wantErr: "cannot parse"},
		{name: "unquoted_string_rejected", doc: `2024-01-02T15:04:05Z`, wantErr: "invalid character"},
		{name: "garbage_rejected", doc: `"nope"`, wantErr: "cannot parse"},
		{name: "four_byte_number_rejected", doc: `1234`, wantErr: "cannot parse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var nt NullTime
			err := json.Unmarshal([]byte(tc.doc), &nt)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected a parse error for %s, got %+v", tc.doc, nt)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				if nt.Valid {
					t.Errorf("a failed unmarshal must leave the value invalid: %+v", nt)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !nt.Valid {
				t.Fatalf("value is invalid after %s", tc.doc)
			}
			if got := nt.Time.Unix(); got != tc.wantUnix {
				t.Errorf("unix time = %d, want %d (parsed %v)", got, tc.wantUnix, nt.Time)
			}
		})
	}
}

func TestNullTimeUnmarshalJSONNullAndShortInputs(t *testing.T) {
	// Documented defect: the JSON literal null is not special-cased. It is four
	// bytes long, so it reaches time.Parse and fails - json.Unmarshal returns an
	// error for what every other decoder treats as "no value". Inputs SHORTER
	// than three bytes take the early return at nulltime.go:37-40 and are
	// silently accepted as "invalid", so `""` and `1` behave differently from
	// `null` even though all three mean the same thing to a caller.
	t.Run("null_is_an_error", func(t *testing.T) {
		var nt NullTime
		err := json.Unmarshal([]byte(`null`), &nt)
		if err == nil {
			t.Fatal("expected the null literal to fail; update this test if it is fixed")
		}
		if !strings.Contains(err.Error(), "parsing time") {
			t.Errorf("error = %v, want a time.Parse error", err)
		}
		if nt.Valid {
			t.Error("value is valid after a failed parse")
		}
	})

	t.Run("null_inside_a_struct_fails_the_whole_document", func(t *testing.T) {
		type doc struct {
			T NullTime `json:"t"`
		}
		var d doc
		err := json.Unmarshal([]byte(`{"t":null}`), &d)
		if err == nil {
			t.Fatal("expected the whole document to fail on a null timestamp")
		}
		if !strings.Contains(err.Error(), "parsing time") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("short_inputs_are_silently_invalid", func(t *testing.T) {
		for _, in := range []string{`""`, `1`, `0`} {
			var nt NullTime
			if err := json.Unmarshal([]byte(in), &nt); err != nil {
				t.Errorf("json.Unmarshal(%s) returned %v; inputs under three bytes take the silent path", in, err)
			}
			if nt.Valid {
				t.Errorf("json.Unmarshal(%s) produced a valid value", in)
			}
		}
	})

	t.Run("null_does_not_need_a_prior_valid_value", func(t *testing.T) {
		// Even a freshly parsed value cannot be cleared with null.
		nt := NullTime{Time: time.Now(), Valid: true}
		if err := json.Unmarshal([]byte(`null`), &nt); err == nil {
			t.Errorf("expected an error; got %+v", nt)
		}
	})
}

func TestNullTimeJSONRoundTrip(t *testing.T) {
	base := time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC)
	in := NullTime{Time: base, Valid: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var out NullTime
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("json.Unmarshal(%s): %v", b, err)
	}
	if !out.Valid || !out.Time.Equal(base) {
		t.Errorf("round trip = %+v, want %v", out, base)
	}
	// Instants survive the round trip even when the encoding normalises the zone.
	if !out.Time.UTC().Equal(base.UTC()) {
		t.Errorf("instants differ: %v != %v", out.Time.UTC(), base.UTC())
	}
}

func TestNullTimeNow(t *testing.T) {
	before := time.Now()
	got := NullTimeNow()
	after := time.Now()

	if !got.Valid {
		t.Fatal("NullTimeNow must be valid")
	}
	if got.Time.Before(before.Add(-time.Second)) || got.Time.After(after.Add(time.Second)) {
		t.Errorf("NullTimeNow = %v, want a time between %v and %v", got.Time, before, after)
	}
}

// ---------------------------------------------------------------------------
// Cross-type consistency
// ---------------------------------------------------------------------------

func TestNullTypesMarshalInvalidAsBareNull(t *testing.T) {
	// Every null* type reports an unset value to JSON as the literal null, so
	// column-shaped documents stay uniform (SQL NULL -> JSON null).
	cases := []struct {
		name string
		in   any
	}{
		{name: "NullString", in: NullString{}},
		{name: "NullInt64", in: NullInt64{}},
		{name: "NullTime", in: NullTime{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != "null" {
				t.Errorf("invalid %s marshals as %s, want null", tc.name, got)
			}
		})
	}
}

func TestNullTimeErrorIsAParseError(t *testing.T) {
	var nt NullTime
	err := json.Unmarshal([]byte(`"not-a-time"`), &nt)
	if err == nil {
		t.Fatal("expected an error")
	}
	var perr *time.ParseError
	if !errors.As(err, &perr) {
		t.Errorf("error %v (%T) is not a *time.ParseError", err, err)
	}
}
