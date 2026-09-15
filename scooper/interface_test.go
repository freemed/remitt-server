package scooper

import (
	"context"
	"encoding/json"
	"testing"
)

// Compile-time contract: every scooper implementation must satisfy Scooper.
var (
	_ Scooper = (*SftpScooper)(nil)
	_ Scooper = (*GatewayEdiSftpScooper)(nil)
)

// TestScooperInterface_ConstantsMatchJavaPluginContract pins the Java-style
// class names and config keys carried over from the 0.5.x reference
// (org/remitt/plugin/scooper/SftpScooper.java:46-53 and
// GatewayEdiSftpScooper.java:38-52). tUserConfig keys and tScooper.scooperClass
// values are matched against these strings across the application, so they are
// wire contract, not implementation detail.
func TestScooperInterface_ConstantsMatchJavaPluginContract(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"SftpScooperClass", SftpScooperClass, "org.remitt.plugin.scooper.SftpScooper"},
		{"SftpScooperEnabled", SftpScooperEnabled, "org.remitt.plugin.scooper.SftpScooper.enabled"},
		{"GatewayEdiScooperClass", GatewayEdiScooperClass, "org.remitt.plugin.scooper.GatewayEdiSftpScooper"},
		{"GatewayEdiScooperEnabled", GatewayEdiScooperEnabled, "org.remitt.plugin.scooper.GatewayEdiSftpScooper.enabled"},
		{"GatewayEdiKeyName", GatewayEdiKeyName, "GatewayEDI"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q; want %q", tt.name, tt.got, tt.want)
			}
		})
	}

	t.Run("enabledKeysAreDerivedFromClassNames", func(t *testing.T) {
		if SftpScooperEnabled != SftpScooperClass+".enabled" {
			t.Errorf("SftpScooperEnabled = %q; want %q", SftpScooperEnabled, SftpScooperClass+".enabled")
		}
		if GatewayEdiScooperEnabled != GatewayEdiScooperClass+".enabled" {
			t.Errorf("GatewayEdiScooperEnabled = %q; want %q", GatewayEdiScooperEnabled, GatewayEdiScooperClass+".enabled")
		}
	})

	t.Run("gatewayEdiEnabledIsNotTheBaseKey", func(t *testing.T) {
		if GatewayEdiScooperEnabled == SftpScooperEnabled {
			t.Error("GatewayEDI scooper and SftpScooper report the same enabled key; they would be enabled/disabled together")
		}
	})
}

// TestScooperResult_JsonContract pins the JSON shape of the values handed back
// to callers: field order, key spelling and []byte content as base64.
func TestScooperResult_JsonContract(t *testing.T) {
	tests := []struct {
		name    string
		result  ScooperResult
		wantRaw string
	}{
		{
			name: "populated",
			result: ScooperResult{
				Filename: "remit.pgp",
				Host:     "sftp.example.invalid",
				Path:     "remits",
				Content:  []byte("hello"),
			},
			wantRaw: `{"filename":"remit.pgp","host":"sftp.example.invalid","path":"remits","content":"aGVsbG8="}`,
		},
		{
			name:    "zeroValue",
			result:  ScooperResult{},
			wantRaw: `{"filename":"","host":"","path":"","content":null}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.result)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(raw) != tt.wantRaw {
				t.Errorf("json.Marshal = %s; want %s", raw, tt.wantRaw)
			}

			var back ScooperResult
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("json.Unmarshal(%s): %v", raw, err)
			}
			if back.Filename != tt.result.Filename || back.Host != tt.result.Host ||
				back.Path != tt.result.Path || string(back.Content) != string(tt.result.Content) {
				t.Errorf("round trip changed the value: got %+v; want %+v", back, tt.result)
			}
		})
	}
}

// TestScooperInterface_MethodsWithoutDatabase covers the interface methods
// that must work with no database, no configuration and no remote host.
func TestScooperInterface_MethodsWithoutDatabase(t *testing.T) {
	tests := []struct {
		name        string
		newScooper  func() Scooper
		wantEnabled string
	}{
		{
			name:        "SftpScooper",
			newScooper:  func() Scooper { return &SftpScooper{} },
			wantEnabled: SftpScooperEnabled,
		},
		{
			name:        "GatewayEdiSftpScooper",
			newScooper:  func() Scooper { return &GatewayEdiSftpScooper{} },
			wantEnabled: GatewayEdiScooperEnabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.newScooper()

			t.Run("GetEnabledConfigValue", func(t *testing.T) {
				if got := s.GetEnabledConfigValue(); got != tt.wantEnabled {
					t.Errorf("GetEnabledConfigValue() = %q; want %q", got, tt.wantEnabled)
				}
			})

			t.Run("SetUsername", func(t *testing.T) {
				if err := s.SetUsername("user1"); err != nil {
					t.Errorf("SetUsername(user1) error = %v; want nil", err)
				}
			})

			t.Run("SetContext", func(t *testing.T) {
				ctx := context.WithValue(context.Background(), ctxTestKey{}, "marker")
				if err := s.SetContext(ctx); err != nil {
					t.Errorf("SetContext() error = %v; want nil", err)
				}
			})

			t.Run("SetParameters", func(t *testing.T) {
				if err := s.SetParameters(map[string]string{}); err != nil {
					t.Errorf("SetParameters(map) error = %v; want nil", err)
				}
			})
		})
	}
}

type ctxTestKey struct{}

// TestGatewayEdiSftpScooper_DelegatesToEmbeddedSftpScooper pins that the
// GatewayEDI scooper shares state with the SftpScooper it embeds, rather than
// keeping its own copy: SetContext and SetUsername must land on the embedded
// value (gatewayedi.go:57 and the promoted SetUsername).
func TestGatewayEdiSftpScooper_DelegatesToEmbeddedSftpScooper(t *testing.T) {
	g := &GatewayEdiSftpScooper{}
	ctx := context.WithValue(context.Background(), ctxTestKey{}, "embedded-marker")

	if err := g.SetContext(ctx); err != nil {
		t.Fatalf("SetContext() error = %v; want nil", err)
	}
	if g.SftpScooper.ctx != ctx {
		t.Error("SetContext did not store the context on the embedded SftpScooper")
	}

	if err := g.SetUsername("user7"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}
	if g.SftpScooper.username != "user7" {
		t.Errorf("embedded username = %q; want %q", g.SftpScooper.username, "user7")
	}

	if err := g.SetParameters(map[string]string{"sftpHost": "sftp.example.invalid", "sftpPort": "22"}); err != nil {
		t.Fatalf("SetParameters() error = %v; want nil", err)
	}
	if g.SftpScooper.host != "sftp.example.invalid" || g.SftpScooper.port != 22 {
		t.Errorf("embedded connection parameters = %s:%d; want sftp.example.invalid:22", g.SftpScooper.host, g.SftpScooper.port)
	}
}

// TestScooperSetters_AlwaysReturnNil documents the current contract: the
// configuration setters never validate and never report an error, so a
// mistyped parameter map only surfaces much later, at Scoop() time, as
// "host/port not configured". Callers cannot rely on SetParameters to reject
// bad configuration (sftp.go:159-175, map contract for the plugin loader).
func TestScooperSetters_AlwaysReturnNil(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]string
	}{
		{"nilMap", nil},
		{"garbagePort", map[string]string{"sftpHost": "sftp.example.invalid", "sftpPort": "not-a-port"}},
		{"negativePort", map[string]string{"sftpHost": "sftp.example.invalid", "sftpPort": "-1"}},
		{"unknownKeysOnly", map[string]string{"whoKnows": "value"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SftpScooper{}
			if err := s.SetParameters(tt.params); err != nil {
				t.Errorf("SetParameters(%v) error = %v; want nil (setters never validate)", tt.params, err)
			}
		})
	}
}
