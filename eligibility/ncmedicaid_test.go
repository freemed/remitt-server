package eligibility

import (
	"context"
	"testing"
)

func TestNCMedicaidEligibilityRegistration(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.NCMedicaidEligibility")
	if err != nil {
		t.Fatalf("expected checker to be registered, got error: %v", err)
	}
	if checker == nil {
		t.Fatal("expected non-nil checker from registry")
	}
}

func TestNCMedicaidEligibilityGetPluginName(t *testing.T) {
	c := &NCMedicaidEligibility{}
	name := c.GetPluginName()
	if name != "org.remitt.plugin.eligibility.NCMedicaidEligibility" {
		t.Errorf("expected plugin name %q, got %q", "org.remitt.plugin.eligibility.NCMedicaidEligibility", name)
	}
}

func TestNCMedicaidEligibilityGetPluginVersion(t *testing.T) {
	c := &NCMedicaidEligibility{}
	v := c.GetPluginVersion()
	if v == "" {
		t.Error("expected non-empty plugin version")
	}
}

func TestNCMedicaidEligibilityGetPluginConfigurationOptions(t *testing.T) {
	c := &NCMedicaidEligibility{}
	opts := c.GetPluginConfigurationOptions()
	if len(opts) == 0 {
		t.Fatal("expected non-empty configuration options")
	}

	expectedKeys := map[string]bool{
		"ncMedicaidHost":     true,
		"ncMedicaidPort":     true,
		"ncMedicaidUsername": true,
		"ncMedicaidPassword": true,
		"ncMedicaidPath":     true,
	}
	for _, opt := range opts {
		delete(expectedKeys, opt)
	}
	if len(expectedKeys) > 0 {
		t.Errorf("missing expected config options: %v", expectedKeys)
	}
}

func TestNCMedicaidEligibilitySetContext(t *testing.T) {
	c := &NCMedicaidEligibility{}
	ctx := context.Background()
	if err := c.SetContext(ctx); err != nil {
		t.Errorf("SetContext failed: %v", err)
	}
}

func TestNCMedicaidEligibilityCheckEligibilityNoConfig(t *testing.T) {
	c := &NCMedicaidEligibility{}
	_, err := c.CheckEligibility("testuser", map[string]string{}, false, 0)
	if err == nil {
		t.Fatal("expected error when config has not been loaded")
	}
}
