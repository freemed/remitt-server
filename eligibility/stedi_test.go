package eligibility

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestStediRegistration verifies that the StediEligibility plugin
// can be instantiated through the registry.
func TestStediRegistration(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.StediEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate StediEligibility: %v", err)
	}
	if checker == nil {
		t.Fatal("InstantiateChecker returned nil")
	}
}

// TestStediGetPluginName verifies the plugin returns the correct
// Java-style dotted class name.
func TestStediGetPluginName(t *testing.T) {
	c := &StediEligibility{}
	expected := "org.remitt.plugin.eligibility.StediEligibility"
	if got := c.GetPluginName(); got != expected {
		t.Errorf("GetPluginName() = %q, want %q", got, expected)
	}
}

// TestStediGetPluginVersion verifies the plugin returns a non-empty
// version string.
func TestStediGetPluginVersion(t *testing.T) {
	c := &StediEligibility{}
	v := c.GetPluginVersion()
	if v == "" {
		t.Error("GetPluginVersion() returned empty string")
	}
}

// TestStediGetPluginConfigurationOptions verifies the plugin returns
// the expected list of configuration option names.
func TestStediGetPluginConfigurationOptions(t *testing.T) {
	c := &StediEligibility{}
	opts := c.GetPluginConfigurationOptions()
	if len(opts) == 0 {
		t.Fatal("expected non-empty configuration options")
	}

	expectedKeys := map[string]bool{
		"stediApiKey":            true,
		"stediTradingPartnerId":  true,
		"stediProviderNpi":       true,
		"stediProviderTaxId":     true,
	}
	for _, opt := range opts {
		delete(expectedKeys, opt)
	}
	if len(expectedKeys) > 0 {
		t.Errorf("missing expected config options: %v", expectedKeys)
	}
}

// TestStediSetContext verifies that SetContext stores the provided
// context without error.
func TestStediSetContext(t *testing.T) {
	c := &StediEligibility{}
	ctx := context.Background()
	if err := c.SetContext(ctx); err != nil {
		t.Errorf("SetContext() error = %v, want nil", err)
	}
}

// TestStediCheckEligibilityNoConfig verifies that CheckEligibility returns
// an error when no database is available to load configuration.
func TestStediCheckEligibilityNoConfig(t *testing.T) {
	c := &StediEligibility{}
	_, err := c.CheckEligibility("testuser", map[string]string{}, false, 0)
	if err == nil {
		t.Fatal("expected error when config has not been loaded")
	}
}

// newStediForTest returns a concrete *StediEligibility for testing
// unexported methods.
func newStediForTest() (*StediEligibility, error) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.StediEligibility")
	if err != nil {
		return nil, err
	}
	s, ok := checker.(*StediEligibility)
	if !ok {
		return nil, err
	}
	return s, nil
}

// TestStediBuildRequestBody verifies that buildRequestBody produces
// valid JSON with the correct structure and values.
func TestStediBuildRequestBody(t *testing.T) {
	s, err := newStediForTest()
	if err != nil {
		t.Fatalf("failed to get StediEligibility: %v", err)
	}

	// Set up config fields that buildRequestBody relies on.
	s.tradingPartnerId = "BCBSM"
	s.providerNpi = "1234567890"

	values := map[string]string{
		"organizationName": "Test Medical Group",
		"memberId":         "M12345678",
		"firstName":        "JOHN",
		"lastName":         "DOE",
		"dateOfBirth":      "1980-01-15",
		"serviceTypeCodes": "30",
	}

	body, err := s.buildRequestBody(values)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	// Verify it's valid JSON.
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("buildRequestBody() produced invalid JSON: %v", err)
	}

	// Verify tradingPartnerServiceId.
	tpsi, ok := result["tradingPartnerServiceId"].(string)
	if !ok {
		t.Fatal("tradingPartnerServiceId missing or not a string")
	}
	if tpsi != "BCBSM" {
		t.Errorf("tradingPartnerServiceId = %q, want %q", tpsi, "BCBSM")
	}

	// Verify provider block.
	provider, ok := result["provider"].(map[string]interface{})
	if !ok {
		t.Fatal("provider missing or not an object")
	}
	if npi, ok := provider["npi"].(string); !ok || npi != "1234567890" {
		t.Errorf("provider.npi = %v, want %q", npi, "1234567890")
	}
	if orgName, ok := provider["organizationName"].(string); !ok || orgName != "Test Medical Group" {
		t.Errorf("provider.organizationName = %v, want %q", orgName, "Test Medical Group")
	}

	// Verify subscriber block.
	subscriber, ok := result["subscriber"].(map[string]interface{})
	if !ok {
		t.Fatal("subscriber missing or not an object")
	}
	if mid, ok := subscriber["memberId"].(string); !ok || mid != "M12345678" {
		t.Errorf("subscriber.memberId = %v, want %q", mid, "M12345678")
	}
	if fn, ok := subscriber["firstName"].(string); !ok || fn != "JOHN" {
		t.Errorf("subscriber.firstName = %v, want %q", fn, "JOHN")
	}
	if ln, ok := subscriber["lastName"].(string); !ok || ln != "DOE" {
		t.Errorf("subscriber.lastName = %v, want %q", ln, "DOE")
	}
	if dob, ok := subscriber["dateOfBirth"].(string); !ok || dob != "1980-01-15" {
		t.Errorf("subscriber.dateOfBirth = %v, want %q", dob, "1980-01-15")
	}

	// Verify serviceTypeCodes.
	stcs, ok := result["serviceTypeCodes"].([]interface{})
	if !ok {
		t.Fatal("serviceTypeCodes missing or not an array")
	}
	if len(stcs) != 1 || stcs[0].(string) != "30" {
		t.Errorf("serviceTypeCodes = %v, want [\"30\"]", stcs)
	}
}

// TestStediBuildRequestBodyEmpty verifies that buildRequestBody
// works with an empty values map (partial body is still built).
func TestStediBuildRequestBodyEmpty(t *testing.T) {
	s, err := newStediForTest()
	if err != nil {
		t.Fatalf("failed to get StediEligibility: %v", err)
	}

	s.tradingPartnerId = "CMS"
	s.providerNpi = "9876543210"

	body, err := s.buildRequestBody(map[string]string{})
	if err != nil {
		t.Fatalf("buildRequestBody() with empty values error = %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("buildRequestBody() with empty values produced invalid JSON: %v", err)
	}

	if tpsi, ok := result["tradingPartnerServiceId"].(string); !ok || tpsi != "CMS" {
		t.Errorf("tradingPartnerServiceId = %v, want %q", tpsi, "CMS")
	}
}

// TestStediBuildRequestBodyJsonFormat verifies that the JSON output
// is properly formatted and contains all expected top-level keys.
func TestStediBuildRequestBodyJsonFormat(t *testing.T) {
	s, err := newStediForTest()
	if err != nil {
		t.Fatalf("failed to get StediEligibility: %v", err)
	}

	s.tradingPartnerId = "UHCMEDICAID"
	s.providerNpi = "1111111111"

	values := map[string]string{
		"organizationName": "TestOrg",
		"memberId":         "M999",
		"firstName":        "JANE",
		"lastName":         "SMITH",
		"dateOfBirth":      "1990-06-15",
		"serviceTypeCodes": "30,45",
	}

	body, err := s.buildRequestBody(values)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	raw := string(body)

	// Verify all expected top-level keys are present.
	for _, key := range []string{
		`"tradingPartnerServiceId"`,
		`"provider"`,
		`"subscriber"`,
		`"serviceTypeCodes"`,
	} {
		if !strings.Contains(raw, key) {
			t.Errorf("JSON body missing expected key: %s", key)
		}
	}

	// Verify the JSON is minified (single line, no newlines for empty body).
	// With values, it should still be valid compact JSON.
	if strings.Count(raw, "\n") > 0 {
		t.Error("JSON body should be compact (no newlines)")
	}
}
