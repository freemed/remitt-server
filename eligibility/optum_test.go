package eligibility

import (
	"context"
	"testing"
	"time"
)

// --- Registry / interface compliance ---

// TestOptumEligibilityRegistration verifies the plugin is registered and
// can be instantiated through InstantiateChecker.
func TestOptumEligibilityRegistration(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.OptumEligibility")
	if err != nil {
		t.Fatalf("expected checker to be registered, got error: %v", err)
	}
	if checker == nil {
		t.Fatal("expected non-nil checker from registry")
	}
}

// TestOptumEligibilityGetPluginName verifies the plugin returns the correct
// Java-style dotted class name.
func TestOptumEligibilityGetPluginName(t *testing.T) {
	c := &OptumEligibility{}
	name := c.GetPluginName()
	if name != "org.remitt.plugin.eligibility.OptumEligibility" {
		t.Errorf("expected plugin name %q, got %q", "org.remitt.plugin.eligibility.OptumEligibility", name)
	}
}

// TestOptumEligibilityGetPluginVersion verifies the plugin returns a non-empty
// version string.
func TestOptumEligibilityGetPluginVersion(t *testing.T) {
	c := &OptumEligibility{}
	v := c.GetPluginVersion()
	if v == "" {
		t.Error("expected non-empty plugin version")
	}
}

// TestOptumEligibilityGetPluginConfigurationOptions verifies the plugin returns
// the expected set of configuration option names.
func TestOptumEligibilityGetPluginConfigurationOptions(t *testing.T) {
	c := &OptumEligibility{}
	opts := c.GetPluginConfigurationOptions()
	if len(opts) == 0 {
		t.Fatal("expected non-empty configuration options")
	}

	expectedKeys := map[string]bool{
		"optumClientId":          true,
		"optumClientSecret":      true,
		"optumBaseUrl":           true,
		"optumTradingPartnerId":  true,
		"optumProviderNpi":       true,
		"optumProviderTaxId":     true,
	}
	for _, opt := range opts {
		delete(expectedKeys, opt)
	}
	if len(expectedKeys) > 0 {
		t.Errorf("missing expected config options: %v", expectedKeys)
	}
}

// TestOptumEligibilitySetContext verifies SetContext stores the provided
// context without error.
func TestOptumEligibilitySetContext(t *testing.T) {
	c := &OptumEligibility{}
	ctx := context.Background()
	if err := c.SetContext(ctx); err != nil {
		t.Errorf("SetContext failed: %v", err)
	}
}

// TestOptumEligibilityCheckEligibilityNoConfig verifies CheckEligibility
// returns an error when the plugin has not been configured (no DB config
// loaded).
func TestOptumEligibilityCheckEligibilityNoConfig(t *testing.T) {
	c := &OptumEligibility{}
	_, err := c.CheckEligibility("testuser", map[string]string{}, false, 0)
	if err == nil {
		t.Fatal("expected error when config has not been loaded")
	}
}

// --- Token cache ---

// TestTokenCacheSetGet verifies that tokens are stored and retrieved
// correctly.
func TestTokenCacheSetGet(t *testing.T) {
	tc := &tokenCache{}

	// Empty cache returns empty string.
	if got := tc.get(); got != "" {
		t.Errorf("empty cache: expected \"\", got %q", got)
	}

	// Set a valid token.
	tc.set("test-token-123", time.Now().Add(1*time.Hour))
	if got := tc.get(); got != "test-token-123" {
		t.Errorf("after set: expected %q, got %q", "test-token-123", got)
	}
}

// TestTokenCacheExpiry verifies that expired tokens are not returned.
func TestTokenCacheExpiry(t *testing.T) {
	tc := &tokenCache{}

	// Set an already-expired token.
	tc.set("expired-token", time.Now().Add(-1*time.Second))
	if got := tc.get(); got != "" {
		t.Errorf("expired token: expected \"\", got %q", got)
	}

	// Set a token that expires in 1ms.
	tc.set("short-lived", time.Now().Add(1*time.Millisecond))
	time.Sleep(5 * time.Millisecond)
	if got := tc.get(); got != "" {
		t.Errorf("short-lived after expiry: expected \"\", got %q", got)
	}
}

// --- Request body ---

// newOptumForTest returns a concrete *OptumEligibility from the registry for
// white-box tests of unexported methods.
func newOptumForTest(t *testing.T) *OptumEligibility {
	t.Helper()
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.OptumEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate OptumEligibility: %v", err)
	}
	o, ok := checker.(*OptumEligibility)
	if !ok {
		t.Fatal("registry returned non-OptumEligibility")
	}
	return o
}

// TestOptumBuildRequestBody verifies buildRequestBody populates the request
// struct from configured plugin fields and caller-provided values.
func TestOptumBuildRequestBody(t *testing.T) {
	o := newOptumForTest(t)
	o.tradingPartnerId = "BCBSM"
	o.providerNpi = "1234567890"
	o.providerTaxId = "12-3456789"

	values := map[string]string{
		"memberId":          "M12345678",
		"firstName":         "JOHN",
		"lastName":          "DOE",
		"dateOfBirth":       "1980-01-15",
		"serviceTypeCodes":  "30",
		"organizationName":  "Test Medical Group",
	}

	req := o.buildRequestBody(values)

	if req.TradingPartnerServiceId != "BCBSM" {
		t.Errorf("TradingPartnerServiceId = %q, want %q", req.TradingPartnerServiceId, "BCBSM")
	}
	if req.Provider.NPI != "1234567890" {
		t.Errorf("Provider.NPI = %q, want %q", req.Provider.NPI, "1234567890")
	}
	if req.Provider.TaxID != "12-3456789" {
		t.Errorf("Provider.TaxID = %q, want %q", req.Provider.TaxID, "12-3456789")
	}
	if req.Provider.OrganizationName != "Test Medical Group" {
		t.Errorf("Provider.OrganizationName = %q, want %q", req.Provider.OrganizationName, "Test Medical Group")
	}
	if req.Subscriber.MemberID != "M12345678" {
		t.Errorf("Subscriber.MemberID = %q, want %q", req.Subscriber.MemberID, "M12345678")
	}
	if req.Subscriber.FirstName != "JOHN" {
		t.Errorf("Subscriber.FirstName = %q, want %q", req.Subscriber.FirstName, "JOHN")
	}
	if req.Subscriber.LastName != "DOE" {
		t.Errorf("Subscriber.LastName = %q, want %q", req.Subscriber.LastName, "DOE")
	}
	if req.Subscriber.DateOfBirth != "1980-01-15" {
		t.Errorf("Subscriber.DateOfBirth = %q, want %q", req.Subscriber.DateOfBirth, "1980-01-15")
	}
	if len(req.ServiceTypeCodes) != 1 || req.ServiceTypeCodes[0] != "30" {
		t.Errorf("ServiceTypeCodes = %v, want [30]", req.ServiceTypeCodes)
	}
}

// TestOptumBuildRequestBodyDefaultServiceType verifies that when no
// serviceTypeCodes or serviceTypes are in the values map, the default "30"
// (health benefit plan coverage) is used.
func TestOptumBuildRequestBodyDefaultServiceType(t *testing.T) {
	o := newOptumForTest(t)
	o.tradingPartnerId = "TP1"
	o.providerNpi = "NPI1"

	values := map[string]string{
		"memberId": "M1",
	}

	req := o.buildRequestBody(values)

	if len(req.ServiceTypeCodes) != 1 || req.ServiceTypeCodes[0] != "30" {
		t.Errorf("ServiceTypeCodes = %v, want [30]", req.ServiceTypeCodes)
	}
}

// TestOptumBuildRequestBodyServiceTypesFallback verifies the "serviceTypes"
// key is used as a fallback when "serviceTypeCodes" is absent.
func TestOptumBuildRequestBodyServiceTypesFallback(t *testing.T) {
	o := newOptumForTest(t)
	o.tradingPartnerId = "TP1"
	o.providerNpi = "NPI1"

	values := map[string]string{
		"memberId":     "M1",
		"serviceTypes": "47",
	}

	req := o.buildRequestBody(values)

	if len(req.ServiceTypeCodes) != 1 || req.ServiceTypeCodes[0] != "47" {
		t.Errorf("ServiceTypeCodes = %v, want [47]", req.ServiceTypeCodes)
	}
}

// TestOptumParseResponseSuccess verifies parseResponse handles 2xx responses.
func TestOptumParseResponseSuccess(t *testing.T) {
	o := newOptumForTest(t)

	resp, err := o.parseResponse(200, []byte(`{}`))
	if err != nil {
		t.Fatalf("parseResponse error: %v", err)
	}
	if resp.Status != StatusOK {
		t.Errorf("Status = %q, want %q", resp.Status, StatusOK)
	}
	if resp.SuccessCode != SuccessCodeSuccess {
		t.Errorf("SuccessCode = %q, want %q", resp.SuccessCode, SuccessCodeSuccess)
	}
}

// TestOptumParseResponseSuccessWithMessages verifies parseResponse extracts
// messages from the JSON body on success.
func TestOptumParseResponseSuccessWithMessages(t *testing.T) {
	o := newOptumForTest(t)

	resp, err := o.parseResponse(200, []byte(`{"messages":["msg1","msg2"]}`))
	if err != nil {
		t.Fatalf("parseResponse error: %v", err)
	}
	if len(resp.Messages) != 2 {
		t.Errorf("Messages count = %d, want 2", len(resp.Messages))
	}
}

// TestOptumParseResponseError verifies parseResponse returns
// VALIDATION_FAILURE on non-2xx responses.
func TestOptumParseResponseError(t *testing.T) {
	o := newOptumForTest(t)

	resp, err := o.parseResponse(401, []byte(`{"error":"unauthorized"}`))
	if err != nil {
		t.Fatalf("parseResponse error: %v", err)
	}
	if resp.SuccessCode != SuccessCodeValidationFailure {
		t.Errorf("SuccessCode = %q, want %q", resp.SuccessCode, SuccessCodeValidationFailure)
	}
	if len(resp.Messages) == 0 {
		t.Error("expected non-empty messages on error response")
	}
}
