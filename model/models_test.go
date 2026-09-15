// Package model: row models, their JSON contracts, and the pure mapping
// helpers that sit between the sqlc rows and the API.
//
// # Scope
//
// Everything here runs without a database. tuserToModel,
// UserModel.UniqueId, the payload-state vocabulary and the JSON contracts of the
// types the API hands to clients are all pure. The functions that issue SQL are
// collected in TestDatabaseBoundAPIRequiresDatabase, which skips with an
// explicit reason rather than mocking a database.
//
// # Boundary pinned here (this used to be a documented defect)
//
// tuserToModel (user.go:31-43) funnels the nullable user columns through
// NewNullStringValue via nullStringFromSQL, so a user row with a real contact
// email or callback credential comes back carrying both the text and the set
// flag: they marshal as that text, a client can tell "bob@example.com" from "no
// contact email", and a genuinely NULL column still reports itself unset.
// TestTuserToModel. The two URI columns, which go through nullStringToString,
// still collapse NULL and "" to "".
//
// Role is NOT part of that mapping: tUser has no role column (the migration's
// tUser is migrations/001_legacy.up.sql:25-36, and dbgen.Tuser has no Role field
// any more), so a role can only come from tRole via UserModel.attachRoles.
// TestTuserToModel/role_never_comes_from_the_tuser_row pins that.
package model

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/freemed/remitt-server/internal/dbgen"
)

func TestPayloadStateVocabulary(t *testing.T) {
	// These strings are compared against the payloadState column and are part of
	// the documented REMITT protocol, so they are pinned literally.
	cases := []struct {
		name string
		got  string
		want string
	}{
		{name: "valid", got: PayloadStateValid, want: "valid"},
		{name: "failed", got: PayloadStateFailed, want: "failed"},
		{name: "completed", got: PayloadStateCompleted, want: "completed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("constant = %q, want %q", tc.got, tc.want)
			}
		})
	}
	if PayloadStateValid == PayloadStateFailed || PayloadStateValid == PayloadStateCompleted || PayloadStateFailed == PayloadStateCompleted {
		t.Error("the payload states must be distinct")
	}
}

func TestFileListItemJSONContract(t *testing.T) {
	// api/file.go returns []FileListItem straight through c.JSON, so these four
	// keys are the published shape (note the camelCase originalId, which differs
	// from the snake_case used by x12dto.go and UserConfigModel).
	got, err := json.Marshal(FileListItem{
		FileName:   "output.x12",
		FileSize:   1024,
		OriginalID: "42",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"filename":"output.x12","filesize":1024,"inserted":"0001-01-01T00:00:00Z","originalId":"42"}`
	if string(got) != want {
		t.Errorf("JSON:\n got: %s\nwant: %s", got, want)
	}

	var back FileListItem
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if back.FileName != "output.x12" || back.FileSize != 1024 || back.OriginalID != "42" {
		t.Errorf("round trip = %+v", back)
	}

	// The zero value keeps every key present (no omitempty), and a zero
	// time.Time renders as the RFC3339 year-one instant.
	got, err = json.Marshal(FileListItem{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if want := `{"filename":"","filesize":0,"inserted":"0001-01-01T00:00:00Z","originalId":""}`; string(got) != want {
		t.Errorf("zero value JSON = %s, want %s", got, want)
	}
}

func TestUserConfigModelJSONContract(t *testing.T) {
	// This is a cross-process contract: api/config.go serves []UserConfigModel
	// and client/client.go decodes it into the same type
	// (client/client_test.go pins the request side).
	got, err := json.Marshal(UserConfigModel{
		User:      "bob",
		Namespace: "render",
		Option:    "plugin",
		Value:     "fixedformxml",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"user":"bob","namespace":"render","option":"plugin","value":"fixedformxml"}`
	if string(got) != want {
		t.Errorf("JSON:\n got: %s\nwant: %s", got, want)
	}

	var decoded []UserConfigModel
	if err := json.Unmarshal([]byte("["+want+"]"), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(decoded) != 1 || decoded[0] != (UserConfigModel{User: "bob", Namespace: "render", Option: "plugin", Value: "fixedformxml"}) {
		t.Errorf("round trip = %+v", decoded)
	}

	// The db tags name the tuserconfig columns (internal/dbgen/models.go:150).
	typ := reflect.TypeOf(UserConfigModel{})
	for i, want := range []string{"user", "cNamespace", "cOption", "cValue"} {
		if got := typ.Field(i).Tag.Get("db"); got != want {
			t.Errorf("field %d db tag = %q, want %q", i, got, want)
		}
	}
}

func TestUserModelUniqueId(t *testing.T) {
	cases := []int64{0, 1, 42, -1}
	for _, id := range cases {
		u := UserModel{Id: id}
		got := u.UniqueId()
		v, ok := got.(int64)
		if !ok {
			t.Fatalf("UniqueId() returned %T, want int64", got)
		}
		if v != id {
			t.Errorf("UniqueId() = %d, want %d", v, id)
		}
	}
	// It reads the receiver, so a pointer receiver is not required.
	pu := &UserModel{Id: 7}
	if got := pu.UniqueId(); got != any(int64(7)) {
		t.Errorf("UniqueId() on a pointer = %#v", got)
	}
}

func TestTuserToModel(t *testing.T) {
	t.Run("fully_populated_row", func(t *testing.T) {
		row := dbgen.Tuser{
			ID:                     11,
			Username:               "bob",
			Passhash:               "5f4dcc3b5aa765d61d8327deb882cf99",
			Contactemail:           sql.NullString{String: "bob@example.com", Valid: true},
			Callbackserviceuri:     sql.NullString{String: "https://cb.example.com/remitt", Valid: true},
			Callbackservicewsdluri: sql.NullString{String: "https://cb.example.com/remitt?wsdl", Valid: true},
			Callbackusername:       sql.NullString{String: "cbuser", Valid: true},
			Callbackpassword:       sql.NullString{String: "cbpass", Valid: true},
		}
		got := tuserToModel(row)

		if got.Id != 11 || got.Username != "bob" || got.PasswordHash != row.Passhash {
			t.Errorf("identity fields = %+v", got)
		}
		if got.CallbackServiceUri != "https://cb.example.com/remitt" {
			t.Errorf("CallbackServiceUri = %q", got.CallbackServiceUri)
		}
		if got.CallbackServiceWsdlUri != "https://cb.example.com/remitt?wsdl" {
			t.Errorf("CallbackServiceWsdlUri = %q", got.CallbackServiceWsdlUri)
		}

		// A non-NULL column carries its text and reports itself set, so a
		// client can tell "bob@example.com" from "no contact email".
		for _, tc := range []struct {
			name string
			got  NullString
			text string
		}{
			{name: "ContactEmail", got: got.ContactEmail, text: "bob@example.com"},
			{name: "CallbackUsername", got: got.CallbackUsername, text: "cbuser"},
			{name: "CallbackPassword", got: got.CallbackPassword, text: "cbpass"},
		} {
			if tc.got.String != tc.text {
				t.Errorf("%s.String = %q, want %q", tc.name, tc.got.String, tc.text)
			}
			if !tc.got.Valid {
				t.Errorf("%s.Valid is false; a non-NULL column must decode as set", tc.name)
			}
			b, err := json.Marshal(tc.got)
			if err != nil {
				t.Fatalf("json.Marshal(%s): %v", tc.name, err)
			}
			if want := `"` + tc.text + `"`; string(b) != want {
				t.Errorf("%s marshals as %s, want %s", tc.name, b, want)
			}
		}
	})

	t.Run("entirely_null_row", func(t *testing.T) {
		got := tuserToModel(dbgen.Tuser{ID: 3, Username: "nobody", Passhash: "x"})
		if got.Id != 3 || got.Username != "nobody" || got.PasswordHash != "x" {
			t.Errorf("identity fields = %+v", got)
		}
		if got.Role != "" || got.CallbackServiceUri != "" || got.CallbackServiceWsdlUri != "" {
			t.Errorf("NULL columns must map to empty strings: %+v", got)
		}
		for name, ns := range map[string]NullString{
			"ContactEmail":     got.ContactEmail,
			"CallbackUsername": got.CallbackUsername,
			"CallbackPassword": got.CallbackPassword,
		} {
			if ns.Valid || ns.String != "" {
				t.Errorf("%s = %+v, want the zero value", name, ns)
			}
		}
	})

	t.Run("valid_but_empty_columns", func(t *testing.T) {
		// An empty string stored in the database is not NULL, and the two column
		// shapes are distinguishable: the plain-string fields collapse it to ""
		// while the NullString ones keep it as a set, empty value, which is what
		// MarshalJSON renders as the two-character document "".
		row := dbgen.Tuser{
			Contactemail:     sql.NullString{String: "", Valid: true},
			Callbackusername: sql.NullString{String: "", Valid: true},
		}
		got := tuserToModel(row)
		for name, ns := range map[string]NullString{
			"ContactEmail":     got.ContactEmail,
			"CallbackUsername": got.CallbackUsername,
		} {
			if !ns.Valid || ns.String != "" {
				t.Errorf("%s = %+v, want a set, empty value", name, ns)
			}
			b, err := json.Marshal(ns)
			if err != nil {
				t.Fatalf("json.Marshal(%s): %v", name, err)
			}
			if string(b) != `""` {
				t.Errorf("%s marshals as %s, want \"\"", name, b)
			}
		}
	})

	t.Run("password_hash_is_copied_verbatim", func(t *testing.T) {
		// tuserToModel does not hash; it copies what the row holds. Hashing
		// happens on the way in (AddUser/user.go:138).
		row := dbgen.Tuser{Passhash: "5f4dcc3b5aa765d61d8327deb882cf99"}
		if got := tuserToModel(row).PasswordHash; got != row.Passhash {
			t.Errorf("PasswordHash = %q, want %q", got, row.Passhash)
		}
	})

	t.Run("role_never_comes_from_the_tuser_row", func(t *testing.T) {
		// The schema the server applies has no role column on tUser
		// (migrations/001_legacy.up.sql:25-36), so dbgen.Tuser has no Role field
		// to read and a mapped row must never carry a role. The role comes from
		// tRole instead (UserModel.attachRoles -> dbgen.GetRolesByName), which is
		// what the Java's UserManagement does with GROUP_CONCAT over a tRole
		// join. This test is the compile-time half of that: the row type has no
		// such field, so a Role populated from a user row is unrepresentable.
		got := tuserToModel(dbgen.Tuser{ID: 1, Username: "Administrator"})
		if got.Role != "" {
			t.Errorf("Role = %q, want the empty string: tUser has no role column", got.Role)
		}
		// ... and the field still exists for the API's role input to land in and
		// for attachRoles to fill (api/user.go UserAdd, model/user.go AddUser).
		u := UserModel{Role: "default"}
		if u.Role != "default" {
			t.Errorf("Role is not settable: %+v", u)
		}
	})
}

func TestUserModelJSONShape(t *testing.T) {
	// UserModel has no JSON tags: the API copies its fields explicitly, and the
	// NullString members marshal as null when unset.
	b, err := json.Marshal(UserModel{Id: 1, Username: "bob", Role: "admin"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"Id":1,"Username":"bob","PasswordHash":"","Role":"admin","ContactEmail":null,"CallbackServiceUri":"","CallbackServiceWsdlUri":"","CallbackUsername":null,"CallbackPassword":null}`
	if string(b) != want {
		t.Errorf("JSON:\n got: %s\nwant: %s", b, want)
	}
}

func TestRowModelsUseNullTypesForNullableColumns(t *testing.T) {
	// The nullable columns of the row models are typed with the null* wrappers so
	// a NULL is representable and a marshalled row reports it as JSON null.
	cases := []struct {
		typ    reflect.Type
		fields map[string]reflect.Type
	}{
		{
			typ: reflect.TypeOf(PayloadModel{}),
			fields: map[string]reflect.Type{
				"OriginalId": reflect.TypeOf(NullString{}),
				"Payload":    reflect.TypeOf([]byte(nil)),
			},
		},
		{
			typ: reflect.TypeOf(EligibilityJobsModel{}),
			fields: map[string]reflect.Type{
				"Processed": reflect.TypeOf(NullTime{}),
				"Payload":   reflect.TypeOf([]byte(nil)),
				"Response":  reflect.TypeOf([]byte(nil)),
			},
		},
		{
			typ: reflect.TypeOf(ProcessorModel{}),
			fields: map[string]reflect.Type{
				"Start": reflect.TypeOf(NullTime{}),
				"End":   reflect.TypeOf(NullTime{}),
			},
		},
		{
			typ: reflect.TypeOf(UserModel{}),
			fields: map[string]reflect.Type{
				"ContactEmail":     reflect.TypeOf(NullString{}),
				"CallbackUsername": reflect.TypeOf(NullString{}),
				"CallbackPassword": reflect.TypeOf(NullString{}),
			},
		},
		{
			typ: reflect.TypeOf(PluginsModel{}),
			fields: map[string]reflect.Type{
				"InputFormat":  reflect.TypeOf(NullString{}),
				"OutputFormat": reflect.TypeOf(NullString{}),
			},
		},
		{
			typ: reflect.TypeOf(KeyringModel{}),
			fields: map[string]reflect.Type{
				"PrivateKey": reflect.TypeOf([]byte(nil)),
				"PublicKey":  reflect.TypeOf([]byte(nil)),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.typ.Name(), func(t *testing.T) {
			for name, want := range tc.fields {
				f, ok := tc.typ.FieldByName(name)
				if !ok {
					t.Errorf("%s has no field %s", tc.typ.Name(), name)
					continue
				}
				if f.Type != want {
					t.Errorf("%s.%s has type %v, want %v", tc.typ.Name(), name, f.Type, want)
				}
			}
		})
	}
}

func TestNullTimeRowsMarshalAsNull(t *testing.T) {
	// A row with an unset timestamp reports it as JSON null rather than a year-one
	// instant, which is what makes ProcessorModel/EligibilityJobsModel rows safe
	// to hand to a client.
	b, err := json.Marshal(EligibilityJobsModel{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if v, ok := decoded["Processed"]; !ok || v != nil {
		t.Errorf("Processed = %#v (present=%v), want null", v, ok)
	}
	if v, ok := decoded["Inserted"]; !ok || v != "0001-01-01T00:00:00Z" {
		t.Errorf("Inserted = %#v, want the zero time as RFC3339", v)
	}
}

// TestDatabaseBoundAPIRequiresDatabase lists every exported function in this
// package that runs SQL through the package-level Queries handle. They are
// deliberately NOT tested here: model.Queries is a nil *dbgen.Queries unless
// model.InitDb has connected to MySQL, so a call would panic rather than fail,
// and faking a database would test the fake. Each one needs a live server
// (migrations + seed data) to be exercised.
func TestDatabaseBoundAPIRequiresDatabase(t *testing.T) {
	t.Skip("requires a live MySQL database (model.InitDb -> model.Queries); no database is available in this environment, and a mock would not exercise the sqlc statements. Functions needing a DB: " +
		"model.GetUserByName (user.go:60), model.GetUserById (user.go:68), (*UserModel).GetById (user.go:82), (UserModel).GetRoles (user.go:107), " +
		"model.CheckUserPassword (user.go:120), model.BasicAuthCallback (user.go:115), model.AddUser (user.go:137), " +
		"model.GetConfigValues (userconfig.go:17), model.SetConfigValue (userconfig.go:34), " +
		"model.GetPluginOptions (pluginoptions.go:18), model.GetPluginsForCategory (plugins.go:17), " +
		"model.AddKeyToKeyring (keyring.go:18), model.GetKeyringEntry (keyring.go:30), " +
		"model.InitDb (db.go:18) and model.MigrateDb (db.go:32)")
}
