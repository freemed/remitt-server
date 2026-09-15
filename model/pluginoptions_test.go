// Package model: plugin-option models and the NullString <-> sql.NullString
// conversion helpers (pluginoptions.go, pluginoptiontransform.go, plugins.go,
// translation.go).
//
// # Scope
//
// The lookup functions (GetPluginOptions, GetPluginsForCategory,
// GetConfigValues, ...) run SQL through the package-level model.Queries and are
// skipped here - see TestDatabaseBoundAPIRequiresDatabase in models_test.go.
// What IS testable without a database is everything the queries hand back
// through: the row structs, their tags, and the conversion helpers
// nullStringFromSQL / nullStringToSQL / nullStringToString / stringToNullString.
//
// # Defect documented here (pinned, not fixed)
//
// NewNullStringValue (nullstring.go:16-20) sets String but leaves Valid false,
// and nullStringFromSQL (plugins.go:37-42) builds every positive result with it.
// The consequences, all pinned below:
//
//   - json.Marshal(NewNullStringValue("x")) is the literal null, not "x";
//   - nullStringFromSQL(sql.NullString{String: "x", Valid: true}) returns a
//     value that marshals as null, so a non-NULL inputformat/outputformat
//     column read through GetPluginsForCategory/GetPluginOptions is reported to
//     clients as unset;
//   - the round trip through nullStringFromSQL(nullStringToSQL(v)) loses the
//     text for any value whose Valid was never set, which is exactly the shape
//     api/file.go and client/client.go exchange.
//
// The fix is one line (`v.Valid = true`), which is why the behaviour is pinned
// rather than worked around.
package model

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
)

func TestNewNullStringValueIsInvalid(t *testing.T) {
	// Documented defect: the constructor does not mark its result valid.
	v := NewNullStringValue("render/text")
	if v.String != "render/text" {
		t.Errorf("String = %q, want render/text", v.String)
	}
	if v.Valid {
		t.Fatal("Valid is true; the constructor was fixed - update this test")
	}

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("json.Marshal(NewNullStringValue(%q)) = %s, want null", v.String, b)
	}

	// The zero value and the constructed value are indistinguishable on the wire.
	zero, err := json.Marshal(NullString{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(zero) != string(b) {
		t.Errorf("constructed value (%s) and zero value (%s) differ", b, zero)
	}
}

func TestNullStringFromSQL(t *testing.T) {
	cases := []struct {
		name      string
		in        sql.NullString
		wantText  string
		wantValid bool
		wantJSON  string
	}{
		{
			name:      "sql_null",
			in:        sql.NullString{},
			wantText:  "",
			wantValid: false,
			wantJSON:  "null",
		},
		{
			name:     "valid_non_empty",
			in:       sql.NullString{String: "render/text", Valid: true},
			wantText: "render/text",
			// Documented defect: the text is carried but Valid is dropped, so the
			// value reports itself as unset and marshals as null.
			wantValid: false,
			wantJSON:  "null",
		},
		{
			name:      "valid_empty",
			in:        sql.NullString{String: "", Valid: true},
			wantText:  "",
			wantValid: false,
			wantJSON:  "null",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nullStringFromSQL(tc.in)
			if got.String != tc.wantText {
				t.Errorf("String = %q, want %q", got.String, tc.wantText)
			}
			if got.Valid != tc.wantValid {
				t.Errorf("Valid = %v, want %v", got.Valid, tc.wantValid)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(b) != tc.wantJSON {
				t.Errorf("json.Marshal = %s, want %s", b, tc.wantJSON)
			}
		})
	}
}

func TestNullStringToSQL(t *testing.T) {
	cases := []struct {
		name      string
		in        NullString
		wantText  string
		wantValid bool
	}{
		{name: "zero_value", in: NullString{}, wantText: "", wantValid: false},
		{name: "valid", in: validNullString("sftp"), wantText: "sftp", wantValid: true},
		{
			name:      "text_without_valid_is_dropped",
			in:        NewNullStringValue("sftp"),
			wantText:  "",
			wantValid: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nullStringToSQL(tc.in)
			if got.String != tc.wantText || got.Valid != tc.wantValid {
				t.Errorf("nullStringToSQL = {%q %v}, want {%q %v}", got.String, got.Valid, tc.wantText, tc.wantValid)
			}
		})
	}
}

func TestNullStringHelperRoundTrips(t *testing.T) {
	t.Run("sql_to_model_to_sql_loses_valid_rows", func(t *testing.T) {
		// Documented defect chain: nullStringFromSQL routes a valid row through
		// NewNullStringValue, which drops Valid; nullStringToSQL then treats the
		// model value as unset and returns SQL NULL. The stored text is gone.
		in := sql.NullString{String: "text", Valid: true}
		out := nullStringToSQL(nullStringFromSQL(in))
		if out != (sql.NullString{}) {
			t.Fatalf("round trip = %+v; if the Valid flag is now preserved, update this test", out)
		}
	})

	t.Run("model_to_sql_keeps_text_but_model_return_loses_valid", func(t *testing.T) {
		in := validNullString("text")
		viaSQL := nullStringToSQL(in)
		if viaSQL != (sql.NullString{String: "text", Valid: true}) {
			t.Fatalf("forward conversion = %+v, want the text and the flag", viaSQL)
		}
		out := nullStringFromSQL(viaSQL)
		if out.String != "text" {
			t.Errorf("text = %q, want text", out.String)
		}
		if out.Valid {
			t.Error("Valid survived; update this test if nullStringFromSQL was fixed")
		}
	})

	t.Run("sql_null_survives_both_directions", func(t *testing.T) {
		// The NULL case is the one the helpers get right.
		if got := nullStringToSQL(nullStringFromSQL(sql.NullString{})); got.Valid || got.String != "" {
			t.Errorf("NULL round trip = %+v, want the zero value", got)
		}
	})
}

func TestNullStringToString(t *testing.T) {
	cases := []struct {
		name string
		in   sql.NullString
		want string
	}{
		{name: "null_is_empty", in: sql.NullString{}, want: ""},
		{name: "valid_value", in: sql.NullString{String: "admin", Valid: true}, want: "admin"},
		{name: "valid_empty_string", in: sql.NullString{String: "", Valid: true}, want: ""},
		{name: "text_without_valid_is_ignored", in: sql.NullString{String: "ignored"}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nullStringToString(tc.in); got != tc.want {
				t.Errorf("nullStringToString(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestStringToNullString(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantValid bool
	}{
		{name: "empty_is_null", in: "", wantValid: false},
		{name: "value_is_valid", in: "admin", wantValid: true},
		{name: "whitespace_only_is_a_value", in: " ", wantValid: true},
		{name: "zero_byte_is_a_value", in: "\x00", wantValid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stringToNullString(tc.in)
			if got.String != tc.in || got.Valid != tc.wantValid {
				t.Errorf("stringToNullString(%q) = %+v, want {%q %v}", tc.in, got, tc.in, tc.wantValid)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Plugin option row structs
// ---------------------------------------------------------------------------

func TestPluginOptionsModelShape(t *testing.T) {
	// The struct mirrors the tpluginoption columns (internal/dbgen/models.go:79,
	// sql/plugins.sql) and carries no JSON tags: the API layer copies the fields
	// by hand, so the field names themselves are the contract.
	want := []struct {
		name string
		typ  reflect.Type
		tag  string
	}{
		{"PluginOption", reflect.TypeOf(""), "poption"},
		{"Plugin", reflect.TypeOf(""), "plugin"},
		{"FullName", reflect.TypeOf(""), "fullname"},
		{"Version", reflect.TypeOf(""), "version"},
		{"Author", reflect.TypeOf(""), "author"},
		{"Category", reflect.TypeOf(""), "category"},
		{"InputFormat", reflect.TypeOf(NullString{}), "inputFormat"},
		{"OutputFormat", reflect.TypeOf(NullString{}), "outputFormat"},
	}

	typ := reflect.TypeOf(PluginOptionsModel{})
	if typ.NumField() != len(want) {
		t.Fatalf("PluginOptionsModel has %d fields, want %d", typ.NumField(), len(want))
	}
	for i, w := range want {
		f := typ.Field(i)
		if f.Name != w.name {
			t.Errorf("field %d is %s, want %s", i, f.Name, w.name)
		}
		if f.Type != w.typ {
			t.Errorf("field %s has type %v, want %v", f.Name, f.Type, w.typ)
		}
		if got := f.Tag.Get("db"); got != w.tag {
			t.Errorf("field %s db tag = %q, want %q", f.Name, got, w.tag)
		}
		if got := f.Tag.Get("json"); got != "" {
			t.Errorf("field %s has an unexpected json tag %q; the API copies fields explicitly", f.Name, got)
		}
	}
}

func TestPluginOptionsModelJSON(t *testing.T) {
	// Without JSON tags the field names are used verbatim, and the NullString
	// fields marshal through their own MarshalJSON.
	got, err := json.Marshal(PluginOptionsModel{
		PluginOption: "text",
		Plugin:       "fixedformxml",
		FullName:     "Fixed Form XML",
		Version:      "1.0",
		Author:       "REMITT",
		Category:     "render",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"PluginOption":"text","Plugin":"fixedformxml","FullName":"Fixed Form XML","Version":"1.0","Author":"REMITT","Category":"render","InputFormat":null,"OutputFormat":null}`
	if string(got) != want {
		t.Errorf("JSON:\n got: %s\nwant: %s", got, want)
	}

	// A helper-built InputFormat is indistinguishable from an unset one.
	got, err = json.Marshal(PluginOptionsModel{InputFormat: nullStringFromSQL(sql.NullString{String: "x12xml", Valid: true})})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if want := `"InputFormat":null`; !containsJSONFragment(t, string(got), want) {
		t.Errorf("a non-NULL column is reported as unset: %s", got)
	}
}

func TestPluginOptionTransformModelShape(t *testing.T) {
	// Mirrors tpluginoptiontransform (internal/dbgen/models.go:90).
	want := []struct {
		name string
		tag  string
	}{
		{"PluginOptionOld", "poptionold"},
		{"PluginOption", "poption"},
		{"Plugin", "plugin"},
	}

	typ := reflect.TypeOf(PluginOptionTransformModel{})
	if typ.NumField() != len(want) {
		t.Fatalf("PluginOptionTransformModel has %d fields, want %d", typ.NumField(), len(want))
	}
	for i, w := range want {
		f := typ.Field(i)
		if f.Name != w.name {
			t.Errorf("field %d is %s, want %s", i, f.Name, w.name)
		}
		if f.Type != reflect.TypeOf("") {
			t.Errorf("field %s has type %v, want string", f.Name, f.Type)
		}
		if got := f.Tag.Get("db"); got != w.tag {
			t.Errorf("field %s db tag = %q, want %q", f.Name, got, w.tag)
		}
	}
}

func TestPluginOptionTransformModelJSON(t *testing.T) {
	got, err := json.Marshal(PluginOptionTransformModel{
		PluginOptionOld: "plaintext",
		PluginOption:    "text",
		Plugin:          "fixedformxml",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"PluginOptionOld":"plaintext","PluginOption":"text","Plugin":"fixedformxml"}`
	if string(got) != want {
		t.Errorf("JSON:\n got: %s\nwant: %s", got, want)
	}

	var back PluginOptionTransformModel
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if back.PluginOptionOld != "plaintext" || back.PluginOption != "text" || back.Plugin != "fixedformxml" {
		t.Errorf("round trip = %+v", back)
	}
}

func TestTranslationModelShape(t *testing.T) {
	// Mirrors ttranslation; unlike the sqlc row type, whose format columns are
	// plain strings (internal/dbgen/models.go:132-136), the model uses NullString
	// so the API can report "no format" as JSON null.
	typ := reflect.TypeOf(TranslationModel{})
	if typ.NumField() != 3 {
		t.Fatalf("TranslationModel has %d fields, want 3", typ.NumField())
	}
	if got := typ.Field(0); got.Name != "Plugin" || got.Type != reflect.TypeOf("") || got.Tag.Get("db") != "plugin" {
		t.Errorf("field 0 = %v", got)
	}
	if got := typ.Field(1); got.Name != "InputFormat" || got.Type != reflect.TypeOf(NullString{}) || got.Tag.Get("db") != "inputFormat" {
		t.Errorf("field 1 = %v", got)
	}
	if got := typ.Field(2); got.Name != "OutputFormat" || got.Type != reflect.TypeOf(NullString{}) || got.Tag.Get("db") != "outputFormat" {
		t.Errorf("field 2 = %v", got)
	}

	b, err := json.Marshal(TranslationModel{Plugin: "x12xml"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if want := `{"Plugin":"x12xml","InputFormat":null,"OutputFormat":null}`; string(b) != want {
		t.Errorf("JSON = %s, want %s", b, want)
	}
}

// containsJSONFragment reports whether frag appears in doc, and fails the test
// with a readable message when it does not.
func containsJSONFragment(t *testing.T, doc, frag string) bool {
	t.Helper()
	for i := 0; i+len(frag) <= len(doc); i++ {
		if doc[i:i+len(frag)] == frag {
			return true
		}
	}
	return false
}
