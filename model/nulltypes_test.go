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
// # Boundary pinned here (each of these used to be a documented defect)
//
//  1. NullString.UnmarshalJSON is declared on a POINTER receiver, so decoding
//     into a *NullString - including into any struct field, which is how
//     api/payload.go:26 and api/api.go decode payloads - writes the document
//     through. A JSON string marks the value set; the literal null clears it;
//     number, bool, array and object are still errors.
//     TestNullStringUnmarshalJSONIsANoOp (historical name: the test used to pin
//     the silent no-op a value receiver made of every decode).
//  2. NullString.MarshalJSON emits only escapes the JSON grammar defines, so
//     json.Marshal succeeds - and the text survives the round trip - for the
//     control characters MySQL stores that Go's own \a, \v and \xNN escapes
//     would have rendered as invalid JSON.
//     TestNullStringMarshalJSONRejectsGoOnlyEscapes (historical name).
//  3. NullTime.UnmarshalJSON treats the JSON literal null as "no value" and
//     rejects every other document it cannot parse, so short junk (`""`, `1`,
//     `0`) is no longer swallowed as a silent "invalid".
//     TestNullTimeUnmarshalJSONNullAndShortInputs.
//
// The NewNullStringValue constructor (nullstring.go:16-20) and the conversion
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
	// These are the characters Go's own quoting renders with escapes JSON does
	// not define: U+0007 -> \a, U+000B -> \v, and every other unprintable ASCII
	// byte (U+0000-U+0006, U+000E-U+001F and U+007F) as \xNN. MarshalJSON used
	// to emit them verbatim through strconv.QuoteToASCII, and encoding/json
	// then rejected the bytes, so json.Marshal reported an error and produced NO
	// output at all for a value that is perfectly storable in MySQL. It must now
	// emit valid JSON that round-trips the character. (Historical name: the case
	// used to pin the rejection.)
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
			if !json.Valid(raw) {
				t.Fatalf("MarshalJSON(%q) = %s is not valid JSON", tc.in, raw)
			}

			out, err := json.Marshal(ns)
			if err != nil {
				t.Fatalf("json.Marshal(%q) returned %v", tc.in, err)
			}
			if string(out) != string(raw) {
				t.Errorf("json.Marshal = %s, MarshalJSON = %s", out, raw)
			}

			// The character has to survive the round trip, on its own and as a
			// struct field - the failure was contagious, so the fix must not be
			// local either.
			var back NullString
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatalf("json.Unmarshal(%s): %v", out, err)
			}
			if !back.Valid || back.String != tc.in {
				t.Errorf("round trip through NullString = %+v, want %q", back, tc.in)
			}

			type wrapper struct {
				F NullString `json:"f"`
			}
			var w wrapper
			if err := json.Unmarshal([]byte(`{"f":`+string(out)+`}`), &w); err != nil {
				t.Fatalf("json.Unmarshal into a struct field: %v", err)
			}
			if !w.F.Valid || w.F.String != tc.in {
				t.Errorf("round trip through a struct field = %+v, want %q", w.F, tc.in)
			}
		})
	}
}

func TestNullStringUnmarshalJSONIsANoOp(t *testing.T) {
	// UnmarshalJSON is declared on the POINTER receiver, so the decoded value
	// reaches the destination. Three things follow, and this table pins all of
	// them: a JSON string (including "") decodes to that text marked set, the
	// literal null clears the value, and a document the delegate rejects returns
	// its error while leaving the destination untouched - which is how a valid
	// client payload keeps its original_id. (Historical name: this test used to
	// pin the silent no-op a value receiver made of every decode.)
	cases := []struct {
		name      string
		doc       string
		wantText  string
		wantValid bool
		wantError string
	}{
		{name: "string", doc: `"hello"`, wantText: "hello", wantValid: true},
		{name: "empty_string", doc: `""`, wantText: "", wantValid: true},
		{name: "null", doc: `null`, wantText: "", wantValid: false},
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
					t.Fatalf("document %s decoded without an error", tc.doc)
				}
				if !strings.Contains(err.Error(), tc.wantError) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantError)
				}
				if ns.Valid || ns.String != "" {
					t.Fatalf("a rejected document populated the value: %+v", ns)
				}
				return
			}
			if err != nil {
				t.Fatalf("json.Unmarshal(%s) returned %v", tc.doc, err)
			}
			if ns.String != tc.wantText || ns.Valid != tc.wantValid {
				t.Fatalf("document %s decoded to %+v, want {%q %v}", tc.doc, ns, tc.wantText, tc.wantValid)
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
		if !p.OriginalID.Valid || p.OriginalID.String != "T-1" {
			t.Errorf("original_id decoded to %+v, want the text T-1 marked set", p.OriginalID)
		}
	})

	t.Run("an_already_valid_value_is_replaced", func(t *testing.T) {
		ns := validNullString("kept")
		if err := json.Unmarshal([]byte(`"replaced"`), &ns); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if ns.String != "replaced" || !ns.Valid {
			t.Errorf("value = %+v, want the newly decoded text", ns)
		}
	})

	t.Run("a_rejected_document_leaves_the_value_alone", func(t *testing.T) {
		ns := validNullString("kept")
		if err := json.Unmarshal([]byte(`5`), &ns); err == nil {
			t.Fatal("expected an error decoding a number into a NullString")
		}
		if ns.String != "kept" || !ns.Valid {
			t.Errorf("value = %+v, want the pre-existing value untouched", ns)
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
		// The object branch takes the document's own Valid field as authoritative
		// instead of overwriting it with the decode's success (nullint.go:43),
		// so {"Int64":0,"Valid":false} and {} decode as INVALID - nothing has to
		// be present in the document for the result to claim validity.
		{name: "object_form_without_valid_field", doc: `{"Int64":7}`,
			wantInt: 7, wantValid: false},
		{name: "object_form_explicitly_invalid", doc: `{"Int64":0,"Valid":false}`,
			wantInt: 0, wantValid: false},
		{name: "empty_object", doc: `{}`, wantInt: 0, wantValid: false},
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
		// Scan only represents the two driver.Value kinds a DATETIME column can
		// hand back: nil for SQL NULL and time.Time for a parsed value (model
		// db.go sets parseTime=true). Any other type used to clear the field and
		// report SUCCESS - nulltime.go:14-17 was a bare comma-ok assertion, so a
		// string or []byte form silently voided the row's data with no error to
		// notice. It is an error now. (Historical name: this case used to pin
		// the silence.)
		for _, in := range []any{"2024-01-02T15:04:05Z", []byte("2024-01-02 15:04:05"), int64(1704207845), 3.5} {
			var nt NullTime
			if err := nt.Scan(in); err == nil {
				t.Errorf("Scan(%#v) silently accepted a value it cannot represent", in)
			}
			if nt.Valid || !nt.Time.IsZero() {
				t.Errorf("Scan(%#v) left %+v behind after an error", in, nt)
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
	// The JSON literal null is special-cased: it means "no value" and clears the
	// receiver, exactly as it does for the other null* types and for every other
	// decoder. Inputs SHORTER than three bytes no longer take an early return and
	// get silently accepted as "invalid": anything that is not an RFC3339
	// timestamp is a parse error, so `""` and `1` behave like every other
	// unparseable document.
	t.Run("null_is_an_error", func(t *testing.T) {
		// Historical name: null used to be a four-byte document that reached
		// time.Parse and failed; it is now the "no value" document.
		nt := NullTime{Time: time.Now(), Valid: true}
		err := json.Unmarshal([]byte(`null`), &nt)
		if err != nil {
			t.Fatalf("json.Unmarshal(null) returned %v", err)
		}
		if nt.Valid || !nt.Time.IsZero() {
			t.Errorf("value = %+v, want the zero value after a null document", nt)
		}
	})

	t.Run("null_inside_a_struct_fails_the_whole_document", func(t *testing.T) {
		// Historical name: a null timestamp used to fail the whole document.
		type doc struct {
			T NullTime `json:"t"`
		}
		d := doc{T: NullTime{Time: time.Now(), Valid: true}}
		err := json.Unmarshal([]byte(`{"t":null}`), &d)
		if err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if d.T.Valid || !d.T.Time.IsZero() {
			t.Errorf("field = %+v, want the zero value", d.T)
		}
	})

	t.Run("short_inputs_are_silently_invalid", func(t *testing.T) {
		// Historical name: these used to be accepted silently.
		for _, in := range []string{`""`, `1`, `0`} {
			var nt NullTime
			if err := json.Unmarshal([]byte(in), &nt); err == nil {
				t.Errorf("json.Unmarshal(%s) was swallowed; want a parse error", in)
			}
			if nt.Valid {
				t.Errorf("json.Unmarshal(%s) produced a valid value", in)
			}
		}
	})

	t.Run("null_does_not_need_a_prior_valid_value", func(t *testing.T) {
		// A freshly parsed value clears with null just as a stale one does.
		nt := NullTime{Time: time.Now(), Valid: true}
		if err := json.Unmarshal([]byte(`null`), &nt); err != nil {
			t.Fatalf("json.Unmarshal(null): %v", err)
		}
		if nt.Valid {
			t.Errorf("value = %+v, want Valid=false", nt)
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
