package eligibility

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Registration tests
// ---------------------------------------------------------------------------

func TestSftpEligibility_Registration(t *testing.T) {
	const className = "org.remitt.plugin.eligibility.SftpEligibility"

	checker, err := InstantiateChecker(className)
	if err != nil {
		t.Fatalf("expected SftpEligibility to be registered, got error: %v", err)
	}
	if checker == nil {
		t.Fatal("InstantiateChecker returned nil checker")
	}
}

func TestSftpEligibility_Registration_WrongName(t *testing.T) {
	_, err := InstantiateChecker("nonexistent")
	if err == nil {
		t.Fatal("expected error for unregistered checker, got nil")
	}
}

// ---------------------------------------------------------------------------
// Interface method tests (no DB required)
// ---------------------------------------------------------------------------

func TestSftpEligibility_GetPluginName(t *testing.T) {
	const className = "org.remitt.plugin.eligibility.SftpEligibility"

	checker, err := InstantiateChecker(className)
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	name := checker.GetPluginName()
	if name != className {
		t.Errorf("expected plugin name %q, got %q", className, name)
	}
}

func TestSftpEligibility_GetPluginVersion(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.SftpEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	version := checker.GetPluginVersion()
	if version == "" {
		t.Error("expected non-empty version string")
	}
}

func TestSftpEligibility_GetPluginConfigurationOptions(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.SftpEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	opts := checker.GetPluginConfigurationOptions()
	if len(opts) == 0 {
		t.Error("expected non-empty configuration options")
	}

	// Verify expected config keys
	expectedKeys := map[string]bool{
		"sftpHost":     false,
		"sftpPort":     false,
		"sftpUsername": false,
		"sftpPassword": false,
		"sftpPath":     false,
		"sftpKeyName":  false,
	}
	for _, opt := range opts {
		if _, ok := expectedKeys[opt]; ok {
			expectedKeys[opt] = true
		}
	}
	for key, found := range expectedKeys {
		if !found {
			t.Errorf("expected configuration option %q not found", key)
		}
	}
}

func TestSftpEligibility_SetContext(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.SftpEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	ctx := context.Background()
	if err := checker.SetContext(ctx); err != nil {
		t.Errorf("SetContext failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CheckEligibility tests (requires DB; skip if unavailable)
// ---------------------------------------------------------------------------

func TestSftpEligibility_CheckEligibility_NoConfig(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for GetConfigValues): %v", r)
		}
	}()

	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.SftpEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	values := map[string]string{
		"patientName": "John Doe",
		"providerId":  "12345",
	}

	resp, err := checker.CheckEligibility("testuser", values, false, 0)
	if err == nil {
		t.Errorf("expected error when no config is set, got response: %+v", resp)
		return
	}
	t.Logf("expected error (no config): %v", err)
}
