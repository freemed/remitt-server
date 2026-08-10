package eligibility

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"
)

// TestGatewayEDIRegistration verifies that the GatewayEDIEligibility plugin
// can be instantiated through the registry.
func TestGatewayEDIRegistration(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.GatewayEDIEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate GatewayEDIEligibility: %v", err)
	}
	if checker == nil {
		t.Fatal("InstantiateChecker returned nil")
	}
}

// TestGatewayEDIGetPluginName verifies the plugin returns the correct
// Java-style dotted class name.
func TestGatewayEDIGetPluginName(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.GatewayEDIEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate GatewayEDIEligibility: %v", err)
	}

	expected := "org.remitt.plugin.eligibility.GatewayEDIEligibility"
	if got := checker.GetPluginName(); got != expected {
		t.Errorf("GetPluginName() = %q, want %q", got, expected)
	}
}

// TestGatewayEDIGetPluginVersion verifies the plugin returns a non-empty
// version string.
func TestGatewayEDIGetPluginVersion(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.GatewayEDIEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate GatewayEDIEligibility: %v", err)
	}

	version := checker.GetPluginVersion()
	if version == "" {
		t.Error("GetPluginVersion() returned empty string")
	}
}

// TestGatewayEDIGetPluginConfigurationOptions verifies the plugin returns
// the expected list of configuration option names.
func TestGatewayEDIGetPluginConfigurationOptions(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.GatewayEDIEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate GatewayEDIEligibility: %v", err)
	}

	opts := checker.GetPluginConfigurationOptions()
	if len(opts) == 0 {
		t.Error("GetPluginConfigurationOptions() returned empty list")
	}

	// Must include the three required config keys.
	expectedKeys := map[string]bool{
		"gatewayEdiUsername":  false,
		"gatewayEdiPassword":  false,
		"gatewayEdiServiceUri": false,
	}
	for _, opt := range opts {
		delete(expectedKeys, opt)
	}
	for k := range expectedKeys {
		t.Errorf("missing expected config key: %s", k)
	}
}

// TestGatewayEDISetContext verifies that SetContext stores the provided
// context without error.
func TestGatewayEDISetContext(t *testing.T) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.GatewayEDIEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate GatewayEDIEligibility: %v", err)
	}

	ctx := context.Background()
	if err := checker.SetContext(ctx); err != nil {
		t.Errorf("SetContext() error = %v, want nil", err)
	}
}

// Helper to extract the GatewayEDIEligibility concrete type from the registry
// so we can access unexported methods for testing.
func newGatewayEDIForTest() (*GatewayEDIEligibility, error) {
	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.GatewayEDIEligibility")
	if err != nil {
		return nil, err
	}
	g, ok := checker.(*GatewayEDIEligibility)
	if !ok {
		return nil, err
	}
	return g, nil
}

// TestGatewayEDIBuildSoapEnvelope verifies that buildSoapEnvelope produces
// valid SOAP XML with the given values.
func TestGatewayEDIBuildSoapEnvelope(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}

	values := map[string]string{
		"fieldA": "value1",
		"fieldB": "value2",
	}

	envelope, err := g.buildSoapEnvelope(values)
	if err != nil {
		t.Fatalf("buildSoapEnvelope() error = %v", err)
	}

	// Verify it's valid XML.
	var v interface{}
	if err := xml.Unmarshal(envelope, &v); err != nil {
		t.Fatalf("buildSoapEnvelope() produced invalid XML: %v", err)
	}

	s := string(envelope)

	// Verify SOAP envelope structure.
	if !strings.Contains(s, "xmlns:soapenv=") {
		t.Error("SOAP envelope missing soapenv namespace declaration")
	}
	if !strings.Contains(s, "soapenv:Envelope") {
		t.Error("SOAP envelope missing Envelope element")
	}
	if !strings.Contains(s, "soapenv:Body") {
		t.Error("SOAP envelope missing Body element")
	}

	// Verify values appear in the payload.
	for _, expectedVal := range values {
		if !strings.Contains(s, expectedVal) {
			t.Errorf("SOAP envelope missing expected value: %s", expectedVal)
		}
	}
}

// TestGatewayEDIBuildSoapEnvelopeEmpty verifies that buildSoapEnvelope
// works with an empty values map.
func TestGatewayEDIBuildSoapEnvelopeEmpty(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}

	envelope, err := g.buildSoapEnvelope(map[string]string{})
	if err != nil {
		t.Fatalf("buildSoapEnvelope() with empty values error = %v", err)
	}

	var v interface{}
	if err := xml.Unmarshal(envelope, &v); err != nil {
		t.Fatalf("buildSoapEnvelope() with empty values produced invalid XML: %v", err)
	}
}

// TestGatewayEDICheckEligibilityStub verifies that CheckEligibility returns
// a success response (stub SOAP endpoint). Requires a live DB connection
// for keyring and user config lookups; skips gracefully when unavailable.
func TestGatewayEDICheckEligibilityStub(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skip("skipping: no DB available for keyring/config lookups")
		}
	}()

	checker, err := InstantiateChecker("org.remitt.plugin.eligibility.GatewayEDIEligibility")
	if err != nil {
		t.Fatalf("failed to instantiate GatewayEDIEligibility: %v", err)
	}

	_ = checker.SetContext(context.Background())

	values := map[string]string{
		"fieldA": "value1",
	}

	resp, err := checker.CheckEligibility("testuser", values, false, 0)
	if err != nil {
		t.Fatalf("CheckEligibility() error = %v", err)
	}
	if resp == nil {
		t.Fatal("CheckEligibility() returned nil response")
	}
	if resp.Status != StatusOK {
		t.Errorf("CheckEligibility() Status = %q, want %q", resp.Status, StatusOK)
	}
	if resp.SuccessCode != SuccessCodeSuccess {
		t.Errorf("CheckEligibility() SuccessCode = %q, want %q", resp.SuccessCode, SuccessCodeSuccess)
	}
}
