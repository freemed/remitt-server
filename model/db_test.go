package model

import (
	"strings"
	"testing"

	"github.com/freemed/remitt-server/config"
)

// TestDataSourceNameHonorsTheConfiguredHost pins the fix for a configuration
// field that was silently ignored: InitDb built its DSN as
// "user:pass@/db?flags", dropping Database.Host, so the database host could not
// be configured at all and every deployment was pinned to the driver default
// (127.0.0.1:3306). The sample remitt.yml carries tcp(127.0.0.1:3306), which is
// the driver's network(addr) form and belongs exactly where this puts it.
func TestDataSourceNameHonorsTheConfiguredHost(t *testing.T) {
	prev := config.Config
	t.Cleanup(func() { config.Config = prev })

	cases := []struct {
		name    string
		host    string
		wantSub string
	}{
		{"configured tcp host", "tcp(127.0.0.1:3307)", "remitt:remitt@tcp(127.0.0.1:3307)/remitt?"},
		{"configured bare host", "10.1.1.13:3306", "remitt:remitt@10.1.1.13:3306/remitt?"},
		{"empty host keeps the driver default", "", "remitt:remitt@/remitt?"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.AppConfig{}
			cfg.SetDefaults()
			cfg.Database.Host = tc.host
			config.Config = cfg

			dsn := dataSourceName()
			if !strings.HasPrefix(dsn, tc.wantSub) {
				t.Errorf("dataSourceName() = %q; want it to start with %q", dsn, tc.wantSub)
			}
			if !strings.Contains(dsn, DbFlags) {
				t.Errorf("dataSourceName() = %q; the driver flags %q were dropped", dsn, DbFlags)
			}
		})
	}

	// The specific regression: a non-default host must appear in the DSN.
	cfg := &config.AppConfig{}
	cfg.SetDefaults()
	cfg.Database.Host = "tcp(127.0.0.1:3307)"
	config.Config = cfg
	if dsn := dataSourceName(); !strings.Contains(dsn, "3307") {
		t.Errorf("the configured port is absent from the DSN (%q): Database.Host is being ignored again", dsn)
	}
}
