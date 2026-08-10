package eligibility

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Registration tests
// ---------------------------------------------------------------------------

func TestBCBSFhirEligibility_Registration(t *testing.T) {
	const className = "org.remitt.plugin.eligibility.BCBSFhirEligibility"

	checker, err := InstantiateChecker(className)
	if err != nil {
		t.Fatalf("expected BCBSFhirEligibility to be registered, got error: %v", err)
	}
	if checker == nil {
		t.Fatal("InstantiateChecker returned nil checker")
	}
}

// ---------------------------------------------------------------------------
// Interface method tests (no DB required)
// ---------------------------------------------------------------------------

func TestBCBSFhirEligibility_GetPluginName(t *testing.T) {
	const className = "org.remitt.plugin.eligibility.BCBSFhirEligibility"

	checker, err := InstantiateChecker(className)
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	name := checker.GetPluginName()
	if name != className {
		t.Errorf("expected plugin name %q, got %q", className, name)
	}
}

func TestBCBSFhirEligibility_GetPluginVersion(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.BCBSFhirEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	version := checker.GetPluginVersion()
	if version == "" {
		t.Error("expected non-empty version string")
	}
}

func TestBCBSFhirEligibility_GetPluginConfigurationOptions(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.BCBSFhirEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	opts := checker.GetPluginConfigurationOptions()
	if len(opts) == 0 {
		t.Error("expected non-empty configuration options")
	}

	expectedKeys := map[string]bool{
		"bcbsFhirBaseUrl":       false,
		"bcbsFhirClientId":      false,
		"bcbsFhirClientSecret":  false,
		"bcbsFhirInsurerRef":    false,
		"bcbsFhirProviderRef":   false,
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

func TestBCBSFhirEligibility_SetContext(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.BCBSFhirEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	ctx := context.Background()
	if err := checker.SetContext(ctx); err != nil {
		t.Errorf("SetContext failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helper to extract BCBSFhirEligibility concrete type
// ---------------------------------------------------------------------------

func newBCBSFhirForTest(t *testing.T) *BCBSFhirEligibility {
	t.Helper()
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.BCBSFhirEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate BCBSFhirEligibility: %v", err)
	}
	b, ok := checker.(*BCBSFhirEligibility)
	if !ok {
		t.Fatalf("checker is not *BCBSFhirEligibility, got %T", checker)
	}
	return b
}

// ---------------------------------------------------------------------------
// buildFhirRequest tests
// ---------------------------------------------------------------------------

func TestBCBSFhirEligibility_BuildFhirRequest(t *testing.T) {
	b := newBCBSFhirForTest(t)

	values := map[string]string{
		"patientId":    "abc-123",
		"providerId":   "def-456",
		"benefitCode":  "30",
		"insurerRef":   "Organization/bcbs-tn",
		"providerRef":  "Practitioner/def-456",
	}

	fhirJSON, err := b.buildFhirRequest(values)
	if err != nil {
		t.Fatalf("buildFhirRequest() error = %v", err)
	}

	// Verify it's valid JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal(fhirJSON, &parsed); err != nil {
		t.Fatalf("buildFhirRequest() produced invalid JSON: %v", err)
	}

	// Verify resourceType.
	if rt, ok := parsed["resourceType"].(string); !ok || rt != "CoverageEligibilityRequest" {
		t.Errorf("expected resourceType 'CoverageEligibilityRequest', got %v", parsed["resourceType"])
	}

	// Verify status.
	if status, ok := parsed["status"].(string); !ok || status != "active" {
		t.Errorf("expected status 'active', got %v", parsed["status"])
	}

	// Verify purpose.
	purposes, ok := parsed["purpose"].([]interface{})
	if !ok || len(purposes) != 1 {
		t.Errorf("expected purpose array with 1 element, got %v", parsed["purpose"])
	} else if purposes[0] != "benefits" {
		t.Errorf("expected purpose[0] 'benefits', got %v", purposes[0])
	}

	// Verify patient reference.
	if patient, ok := parsed["patient"].(map[string]interface{}); ok {
		if ref, ok := patient["reference"].(string); !ok || ref != "Patient/abc-123" {
			t.Errorf("expected patient reference 'Patient/abc-123', got %v", patient["reference"])
		}
	} else {
		t.Error("missing patient field")
	}

	// Verify insurer reference.
	if insurer, ok := parsed["insurer"].(map[string]interface{}); ok {
		if ref, ok := insurer["reference"].(string); !ok || ref != "Organization/bcbs-tn" {
			t.Errorf("expected insurer reference 'Organization/bcbs-tn', got %v", insurer["reference"])
		}
	} else {
		t.Error("missing insurer field")
	}

	// Verify provider reference.
	if provider, ok := parsed["provider"].(map[string]interface{}); ok {
		if ref, ok := provider["reference"].(string); !ok || ref != "Practitioner/def-456" {
			t.Errorf("expected provider reference 'Practitioner/def-456', got %v", provider["reference"])
		}
	} else {
		t.Error("missing provider field")
	}

	// Verify item array.
	items, ok := parsed["item"].([]interface{})
	if !ok || len(items) != 1 {
		t.Errorf("expected item array with 1 element, got %v", parsed["item"])
	} else {
		item := items[0].(map[string]interface{})
		category := item["category"].(map[string]interface{})
		coding := category["coding"].([]interface{})
		coding0 := coding[0].(map[string]interface{})
		if code := coding0["code"].(string); code != "30" {
			t.Errorf("expected benefit category code '30', got %v", coding0["code"])
		}
	}
}

func TestBCBSFhirEligibility_BuildFhirRequest_MinimalValues(t *testing.T) {
	b := newBCBSFhirForTest(t)

	// Only provide patientId; others use defaults.
	values := map[string]string{
		"patientId": "pat-001",
	}

	fhirJSON, err := b.buildFhirRequest(values)
	if err != nil {
		t.Fatalf("buildFhirRequest() with minimal values error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(fhirJSON, &parsed); err != nil {
		t.Fatalf("buildFhirRequest() produced invalid JSON: %v", err)
	}

	// Should still have required fields with defaults.
	if rt := parsed["resourceType"].(string); rt != "CoverageEligibilityRequest" {
		t.Errorf("expected resourceType 'CoverageEligibilityRequest', got %q", rt)
	}

	// Patient reference should be from values.
	if patient, ok := parsed["patient"].(map[string]interface{}); ok {
		if ref := patient["reference"].(string); ref != "Patient/pat-001" {
			t.Errorf("expected patient reference 'Patient/pat-001', got %q", ref)
		}
	}

	// Should have created timestamp.
	if _, ok := parsed["created"]; !ok {
		t.Error("expected 'created' field in FHIR request")
	}
}

func TestBCBSFhirEligibility_BuildFhirRequest_IncludesID(t *testing.T) {
	b := newBCBSFhirForTest(t)

	values := map[string]string{
		"patientId": "test-patient",
	}

	fhirJSON, err := b.buildFhirRequest(values)
	if err != nil {
		t.Fatalf("buildFhirRequest() error = %v", err)
	}

	s := string(fhirJSON)

	// Should have an id field (non-empty).
	if !strings.Contains(s, `"id"`) {
		t.Error("expected 'id' field in FHIR request JSON")
	}
}

// ---------------------------------------------------------------------------
// parseFhirResponse tests
// ---------------------------------------------------------------------------

func TestBCBSFhirEligibility_ParseFhirResponse_Complete(t *testing.T) {
	b := newBCBSFhirForTest(t)

	responseJSON := []byte(`{
		"resourceType": "CoverageEligibilityResponse",
		"status": "active",
		"purpose": ["benefits"],
		"outcome": "complete",
		"disposition": "Coverage is active",
		"insurance": [{
			"coverage": {"reference": "Coverage/cov-001"},
			"inforce": true,
			"benefitPeriod": {"start": "2024-01-01", "end": "2024-12-31"}
		}]
	}`)

	resp, err := b.parseFhirResponse(responseJSON)
	if err != nil {
		t.Fatalf("parseFhirResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("parseFhirResponse() returned nil response")
	}

	if resp.Status != StatusOK {
		t.Errorf("expected Status %q, got %q", StatusOK, resp.Status)
	}
	if resp.SuccessCode != SuccessCodeSuccess {
		t.Errorf("expected SuccessCode %q, got %q", SuccessCodeSuccess, resp.SuccessCode)
	}
	if len(resp.Messages) == 0 {
		t.Error("expected non-empty Messages")
	} else if resp.Messages[0] != "Coverage is active" {
		t.Errorf("expected disposition message 'Coverage is active', got %q", resp.Messages[0])
	}
}

func TestBCBSFhirEligibility_ParseFhirResponse_Denied(t *testing.T) {
	b := newBCBSFhirForTest(t)

	responseJSON := []byte(`{
		"resourceType": "CoverageEligibilityResponse",
		"status": "active",
		"purpose": ["benefits"],
		"outcome": "error",
		"disposition": "Coverage denied: patient not found",
		"insurance": [{
			"coverage": {"reference": "Coverage/cov-001"},
			"inforce": false,
			"benefitPeriod": {"start": "2024-01-01", "end": "2024-12-31"}
		}]
	}`)

	resp, err := b.parseFhirResponse(responseJSON)
	if err != nil {
		t.Fatalf("parseFhirResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("parseFhirResponse() returned nil response")
	}

	if resp.SuccessCode != SuccessCodeValidationFailure {
		t.Errorf("expected SuccessCode %q, got %q", SuccessCodeValidationFailure, resp.SuccessCode)
	}
}

func TestBCBSFhirEligibility_ParseFhirResponse_NotInForce(t *testing.T) {
	b := newBCBSFhirForTest(t)

	responseJSON := []byte(`{
		"resourceType": "CoverageEligibilityResponse",
		"status": "active",
		"purpose": ["benefits"],
		"outcome": "complete",
		"disposition": "Coverage exists but is inactive",
		"insurance": [{
			"coverage": {"reference": "Coverage/cov-001"},
			"inforce": false,
			"benefitPeriod": {"start": "2024-01-01", "end": "2024-12-31"}
		}]
	}`)

	resp, err := b.parseFhirResponse(responseJSON)
	if err != nil {
		t.Fatalf("parseFhirResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("parseFhirResponse() returned nil response")
	}

	if resp.SuccessCode != SuccessCodeValidationFailure {
		t.Errorf("expected SuccessCode %q for not-inforce, got %q", SuccessCodeValidationFailure, resp.SuccessCode)
	}
}

func TestBCBSFhirEligibility_ParseFhirResponse_NoInsurance(t *testing.T) {
	b := newBCBSFhirForTest(t)

	responseJSON := []byte(`{
		"resourceType": "CoverageEligibilityResponse",
		"status": "active",
		"purpose": ["benefits"],
		"outcome": "complete",
		"disposition": "No coverage found"
	}`)

	resp, err := b.parseFhirResponse(responseJSON)
	if err != nil {
		t.Fatalf("parseFhirResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("parseFhirResponse() returned nil response")
	}

	// No insurance array means not inforce, so validation failure.
	if resp.SuccessCode != SuccessCodeValidationFailure {
		t.Errorf("expected SuccessCode %q with no insurance, got %q", SuccessCodeValidationFailure, resp.SuccessCode)
	}
}

func TestBCBSFhirEligibility_ParseFhirResponse_InvalidJSON(t *testing.T) {
	b := newBCBSFhirForTest(t)

	_, err := b.parseFhirResponse([]byte(`{invalid json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// validateConfig tests
// ---------------------------------------------------------------------------

func TestBCBSFhirEligibility_ValidateConfig(t *testing.T) {
	b := newBCBSFhirForTest(t)

	// Missing required keys.
	err := b.validateConfig(map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty config")
	}

	// Missing only bcbsFhirBaseUrl.
	err = b.validateConfig(map[string]string{
		"bcbsFhirClientId":     "client-id",
		"bcbsFhirClientSecret": "client-secret",
	})
	if err == nil {
		t.Fatal("expected error when bcbsFhirBaseUrl is missing")
	}

	// Missing only bcbsFhirClientId.
	err = b.validateConfig(map[string]string{
		"bcbsFhirBaseUrl":      "https://fhir.example.com",
		"bcbsFhirClientSecret": "client-secret",
	})
	if err == nil {
		t.Fatal("expected error when bcbsFhirClientId is missing")
	}

	// Missing only bcbsFhirClientSecret.
	err = b.validateConfig(map[string]string{
		"bcbsFhirBaseUrl":  "https://fhir.example.com",
		"bcbsFhirClientId": "client-id",
	})
	if err == nil {
		t.Fatal("expected error when bcbsFhirClientSecret is missing")
	}

	// All required keys present (should succeed).
	err = b.validateConfig(map[string]string{
		"bcbsFhirBaseUrl":      "https://fhir.example.com",
		"bcbsFhirClientId":     "client-id",
		"bcbsFhirClientSecret": "client-secret",
	})
	if err != nil {
		t.Errorf("unexpected error for valid config: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CheckEligibility tests (requires DB; skip if unavailable)
// ---------------------------------------------------------------------------

func TestBCBSFhirEligibility_CheckEligibility_NoConfig(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for GetConfigValues): %v", r)
		}
	}()

	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.BCBSFhirEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate: %v", err)
	}

	values := map[string]string{
		"patientId":  "test-patient",
		"providerId": "test-provider",
	}

	resp, err := checker.CheckEligibility("testuser", values, false, 0)
	if err == nil {
		t.Errorf("expected error when no config is set, got response: %+v", resp)
		return
	}
	t.Logf("expected error (no config): %v", err)
}
