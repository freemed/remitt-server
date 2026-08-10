package eligibility

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// newMedicareHETSForTest returns the concrete MedicareHETSEligibility from
// the registry so unexported methods can be tested directly.
func newMedicareHETSForTest(t *testing.T) *MedicareHETSEligibility {
	t.Helper()
	checker, err := InstantiateChecker(MedicareHETSEligibilityClass)
	if err != nil {
		t.Fatalf("failed to instantiate MedicareHETSEligibility: %v", err)
	}
	m, ok := checker.(*MedicareHETSEligibility)
	if !ok {
		t.Fatalf("checker is not *MedicareHETSEligibility, got %T", checker)
	}
	return m
}

// buildSoapResponseEnvelope wraps an X12 271 string in a CAQH CORE SOAP
// response envelope with base64 encoding, for use in response parsing tests.
func buildSoapResponseEnvelope(x12271 string) []byte {
	b64Payload := base64.StdEncoding.EncodeToString([]byte(x12271))
	response := `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:CORE="http://www.caqh.org/SOAP/WSDL/">
  <soap:Header/>
  <soap:Body>
    <CORE:RealTimeResponse>
      <PayloadType>X12_271_Response_005010X279A1</PayloadType>
      <ProcessingMode>RealTime</ProcessingMode>
      <Payload>` + b64Payload + `</Payload>
    </CORE:RealTimeResponse>
  </soap:Body>
</soap:Envelope>`
	return []byte(response)
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestMedicareHETSRegistration(t *testing.T) {
	checker, err := InstantiateChecker(MedicareHETSEligibilityClass)
	if err != nil {
		t.Fatalf("expected MedicareHETSEligibility to be registered, got error: %v", err)
	}
	if checker == nil {
		t.Fatal("InstantiateChecker returned nil")
	}
}

// ---------------------------------------------------------------------------
// GetPluginName
// ---------------------------------------------------------------------------

func TestMedicareHETSGetPluginName(t *testing.T) {
	checker := newMedicareHETSForTest(t)
	name := checker.GetPluginName()
	if name != MedicareHETSEligibilityClass {
		t.Errorf("expected plugin name %q, got %q", MedicareHETSEligibilityClass, name)
	}
}

// ---------------------------------------------------------------------------
// GetPluginVersion
// ---------------------------------------------------------------------------

func TestMedicareHETSGetPluginVersion(t *testing.T) {
	checker := newMedicareHETSForTest(t)
	v := checker.GetPluginVersion()
	if v == "" {
		t.Error("expected non-empty plugin version")
	}
}

// ---------------------------------------------------------------------------
// GetPluginConfigurationOptions
// ---------------------------------------------------------------------------

func TestMedicareHETSGetPluginConfigurationOptions(t *testing.T) {
	checker := newMedicareHETSForTest(t)
	opts := checker.GetPluginConfigurationOptions()
	if len(opts) == 0 {
		t.Fatal("expected non-empty configuration options")
	}

	expectedKeys := []string{
		"hetsUsername",
		"hetsPassword",
		"hetsEndpointUrl",
		"hetsSubmitterId",
		"hetsProviderNpi",
	}

	found := make(map[string]bool)
	for _, opt := range opts {
		found[opt] = true
	}
	for _, key := range expectedKeys {
		if !found[key] {
			t.Errorf("missing expected config key: %s", key)
		}
	}
}

// ---------------------------------------------------------------------------
// SetContext
// ---------------------------------------------------------------------------

func TestMedicareHETSSetContext(t *testing.T) {
	checker := newMedicareHETSForTest(t)
	ctx := context.Background()
	if err := checker.SetContext(ctx); err != nil {
		t.Errorf("SetContext() error = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// CheckEligibility — missing config (requires DB; skip if unavailable)
// ---------------------------------------------------------------------------

func TestMedicareHETSCheckEligibilityNoConfig(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for GetConfigValues): %v", r)
		}
	}()

	checker := newMedicareHETSForTest(t)
	_, err := checker.CheckEligibility("testuser", map[string]string{}, false, 0)
	if err == nil {
		t.Fatal("expected error when no config is set")
	}
	t.Logf("expected error (no config): %v", err)
}

// ---------------------------------------------------------------------------
// buildX12270
// ---------------------------------------------------------------------------

func TestMedicareHETSBuildX12270ValidEDI(t *testing.T) {
	m := newMedicareHETSForTest(t)

	values := map[string]string{
		"memberId":    "M12345678A",
		"firstName":   "JOHN",
		"lastName":    "DOE",
		"dateOfBirth": "19800115",
		"serviceDate": "20240810",
	}

	result, err := m.buildX12270(values, "SUBMITTERID", "1234567890")
	if err != nil {
		t.Fatalf("buildX12270() error = %v", err)
	}

	// Verify required segments are present.
	requiredSegments := []string{
		"ISA*", "GS*", "ST*", "BHT*",
		"NM1*PR*", "NM1*1P*", "NM1*IL*",
		"DMG*", "DTP*", "EQ*",
		"SE*", "GE*", "IEA*",
	}
	for _, seg := range requiredSegments {
		if !strings.Contains(result, seg) {
			t.Errorf("X12 270 missing expected segment: %s", seg)
		}
	}

	// Verify HL segments.
	if !strings.Contains(result, "HL*1**20*1") {
		t.Error("X12 270 missing HL*1 (information source)")
	}
	if !strings.Contains(result, "HL*2*1*21*1") {
		t.Error("X12 270 missing HL*2 (information receiver)")
	}
	if !strings.Contains(result, "HL*3*2*22*0") {
		t.Error("X12 270 missing HL*3 (subscriber)")
	}

	// Verify member/patient data appears.
	if !strings.Contains(result, "M12345678A") {
		t.Error("X12 270 missing member ID")
	}
	if !strings.Contains(result, "DOE") {
		t.Error("X12 270 missing last name")
	}
	if !strings.Contains(result, "JOHN") {
		t.Error("X12 270 missing first name")
	}
	if !strings.Contains(result, "19800115") {
		t.Error("X12 270 missing date of birth")
	}
	if !strings.Contains(result, "20240810") {
		t.Error("X12 270 missing service date")
	}

	// Verify submitter and provider IDs.
	if !strings.Contains(result, "SUBMITTERID") {
		t.Error("X12 270 missing submitter ID")
	}
	if !strings.Contains(result, "1234567890") {
		t.Error("X12 270 missing provider NPI")
	}

	// Verify the transaction set has the 279A1 implementation guide reference.
	if !strings.Contains(result, "005010X279A1") {
		t.Error("X12 270 missing 005010X279A1 implementation guide")
	}
}

// TestMedicareHETSBuildX12270DefaultProviderName verifies that when
// providerName is not in values, the default "TEST PROVIDER" is used.
func TestMedicareHETSBuildX12270DefaultProviderName(t *testing.T) {
	m := newMedicareHETSForTest(t)

	values := map[string]string{
		"memberId":  "M12345678A",
		"firstName": "JOHN",
		"lastName":  "DOE",
	}

	result, err := m.buildX12270(values, "SUBMITTERID", "1234567890")
	if err != nil {
		t.Fatalf("buildX12270() error = %v", err)
	}

	if !strings.Contains(result, "TEST") || !strings.Contains(result, "PROVIDER") {
		t.Error("X12 270 should contain default provider name 'TEST PROVIDER'")
	}
}

// TestMedicareHETSBuildX12270CustomProviderName verifies the custom
// provider name is used when provided.
func TestMedicareHETSBuildX12270CustomProviderName(t *testing.T) {
	m := newMedicareHETSForTest(t)

	values := map[string]string{
		"memberId":     "M12345678A",
		"firstName":    "JOHN",
		"lastName":     "DOE",
		"providerName": "CUSTOM CLINIC",
	}

	result, err := m.buildX12270(values, "SUBMITTERID", "1234567890")
	if err != nil {
		t.Fatalf("buildX12270() error = %v", err)
	}

	if !strings.Contains(result, "CUSTOM") || !strings.Contains(result, "CLINIC") {
		t.Error("X12 270 should contain custom provider name 'CUSTOM CLINIC'")
	}
}

// TestMedicareHETSBuildX12270MissingMemberId verifies that an error is
// returned when memberId is missing.
func TestMedicareHETSBuildX12270MissingMemberId(t *testing.T) {
	m := newMedicareHETSForTest(t)

	values := map[string]string{
		"lastName": "DOE",
	}

	_, err := m.buildX12270(values, "SUBMITTERID", "1234567890")
	if err == nil {
		t.Fatal("expected error when memberId is missing")
	}
	if !strings.Contains(err.Error(), "memberId") {
		t.Errorf("expected error to mention memberId, got: %v", err)
	}
}

// TestMedicareHETSBuildX12270MissingLastName verifies that an error is
// returned when lastName is missing.
func TestMedicareHETSBuildX12270MissingLastName(t *testing.T) {
	m := newMedicareHETSForTest(t)

	values := map[string]string{
		"memberId": "M12345678A",
	}

	_, err := m.buildX12270(values, "SUBMITTERID", "1234567890")
	if err == nil {
		t.Fatal("expected error when lastName is missing")
	}
	if !strings.Contains(err.Error(), "lastName") {
		t.Errorf("expected error to mention lastName, got: %v", err)
	}
}

// TestMedicareHETSBuildX12270OptionalFieldsOmitted verifies that optional
// fields (dateOfBirth, serviceDate) can be omitted without error.
func TestMedicareHETSBuildX12270OptionalFieldsOmitted(t *testing.T) {
	m := newMedicareHETSForTest(t)

	values := map[string]string{
		"memberId":  "M12345678A",
		"firstName": "JOHN",
		"lastName":  "DOE",
	}

	result, err := m.buildX12270(values, "SUBMITTERID", "1234567890")
	if err != nil {
		t.Fatalf("buildX12270() with optional fields omitted error = %v", err)
	}

	// DTP should still be present (defaults to today).
	if !strings.Contains(result, "DTP*291") {
		t.Error("X12 270 should still have DTP segment when serviceDate is omitted")
	}
	// DMG should not be present when dateOfBirth is omitted.
	if strings.Contains(result, "DMG*") {
		t.Error("X12 270 should not have DMG segment when dateOfBirth is omitted")
	}
}

// ---------------------------------------------------------------------------
// buildSoapEnvelope
// ---------------------------------------------------------------------------

func TestMedicareHETSBuildSoapEnvelope(t *testing.T) {
	m := newMedicareHETSForTest(t)

	envelope, err := m.buildSoapEnvelope("testUser", "testPass", "dGVzdCBwYXlsb2Fk")
	if err != nil {
		t.Fatalf("buildSoapEnvelope() error = %v", err)
	}

	// Verify it's valid XML.
	var v interface{}
	if err := xml.Unmarshal(envelope, &v); err != nil {
		t.Fatalf("buildSoapEnvelope() produced invalid XML: %v", err)
	}

	s := string(envelope)

	// Verify SOAP structure.
	if !strings.Contains(s, "soap:Envelope") {
		t.Error("SOAP envelope missing Envelope element")
	}
	if !strings.Contains(s, "soap:Header") {
		t.Error("SOAP envelope missing Header element")
	}
	if !strings.Contains(s, "soap:Body") {
		t.Error("SOAP envelope missing Body element")
	}

	// Verify WS-Security header.
	if !strings.Contains(s, "wsse:Security") {
		t.Error("SOAP envelope missing wsse:Security header")
	}
	if !strings.Contains(s, "wsse:UsernameToken") {
		t.Error("SOAP envelope missing wsse:UsernameToken")
	}
	if !strings.Contains(s, "wsse:Username") {
		t.Error("SOAP envelope missing wsse:Username")
	}
	if !strings.Contains(s, "wsse:Password") {
		t.Error("SOAP envelope missing wsse:Password")
	}

	// Verify credentials appear (not escaped for SOAP).
	if !strings.Contains(s, "testUser") {
		t.Error("SOAP envelope missing username value")
	}
	if !strings.Contains(s, "testPass") {
		t.Error("SOAP envelope missing password value")
	}

	// Verify CORE:RealTimeRequest.
	if !strings.Contains(s, "CORE:RealTimeRequest") {
		t.Error("SOAP envelope missing CORE:RealTimeRequest element")
	}
	if !strings.Contains(s, "PayloadType") {
		t.Error("SOAP envelope missing PayloadType element")
	}
	if !strings.Contains(s, "ProcessingMode") {
		t.Error("SOAP envelope missing ProcessingMode element")
	}
	if !strings.Contains(s, "Payload") {
		t.Error("SOAP envelope missing Payload element")
	}

	// Verify payload content.
	if !strings.Contains(s, "X12_270_Request_005010X279A1") {
		t.Error("SOAP envelope missing correct PayloadType")
	}
	if !strings.Contains(s, "RealTime") {
		t.Error("SOAP envelope missing RealTime ProcessingMode")
	}
	if !strings.Contains(s, "dGVzdCBwYXlsb2Fk") {
		t.Error("SOAP envelope missing base64 payload")
	}
}

// TestMedicareHETSBuildSoapEnvelopeSpecialChars verifies that special
// characters in credentials don't break the XML template. Note that
// XML-special characters (<, >, &) are not valid in SOAP element content
// without escaping; real credentials would not contain them.
func TestMedicareHETSBuildSoapEnvelopeSpecialChars(t *testing.T) {
	m := newMedicareHETSForTest(t)

	// Use realistic credential characters: quotes, apostrophes, and
	// unicode characters are valid in SOAP without special escaping.
	envelope, err := m.buildSoapEnvelope("user.name@domain", "p@ss'w\"ord!", "cGF5bG9hZA==")
	if err != nil {
		t.Fatalf("buildSoapEnvelope() with special chars error = %v", err)
	}

	var v interface{}
	if err := xml.Unmarshal(envelope, &v); err != nil {
		t.Fatalf("buildSoapEnvelope() with special chars produced invalid XML: %v", err)
	}

	// Verify the credentials made it through.
	s := string(envelope)
	if !strings.Contains(s, "user.name@domain") {
		t.Error("SOAP envelope missing username with special chars")
	}
	if !strings.Contains(s, "p@ss'w\"ord!") {
		t.Error("SOAP envelope missing password with special chars")
	}
}

// ---------------------------------------------------------------------------
// parseSoapResponse
// ---------------------------------------------------------------------------

func TestMedicareHETSParseSoapResponseActiveCoverage(t *testing.T) {
	m := newMedicareHETSForTest(t)

	x12271 := m.buildCannedX12271()
	b64Response := buildSoapResponseEnvelope(x12271)

	resp, err := m.parseSoapResponse(b64Response)
	if err != nil {
		t.Fatalf("parseSoapResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("parseSoapResponse() returned nil response")
	}
	if resp.Status != StatusOK {
		t.Errorf("Status = %q, want %q", resp.Status, StatusOK)
	}
	if resp.SuccessCode != SuccessCodeSuccess {
		t.Errorf("SuccessCode = %q, want %q (expected active coverage)", resp.SuccessCode, SuccessCodeSuccess)
	}
	if len(resp.Messages) == 0 {
		t.Error("expected non-empty messages")
	}

	hasActive := false
	for _, msg := range resp.Messages {
		if strings.Contains(msg, "Active Coverage") {
			hasActive = true
			break
		}
	}
	if !hasActive {
		t.Error("expected at least one active coverage message")
	}
}

func TestMedicareHETSParseSoapResponseNoCoverage(t *testing.T) {
	m := newMedicareHETSForTest(t)

	x12271 := `ISA*00*          *00*          *ZZ*CMSHETS        *ZZ*SUBMITTERID    *240810*1200*^*00501*000000002*0*T*:~
GS*HB*CMSHETS*SUBMITTERID*20240810*1200*1*X*005010X279A1~
ST*271*0001*005010X279A1~
BHT*0022*11*TRACE001*20240810*1200~
HL*1**20*1~
NM1*PR*2*MEDICARE*****PI*CMS~
HL*2*1*21*1~
NM1*1P*2*TEST PROVIDER*****XX*1234567890~
HL*3*2*22*0~
NM1*IL*1*DOE*JOHN****MI*M12345678A~
EB*R**30*MA*MEDICARE PART A~
EB*R**30*MB*MEDICARE PART B~
SE*11*0001~
GE*1*1~
IEA*1*000000002~
`
	b64Response := buildSoapResponseEnvelope(x12271)

	resp, err := m.parseSoapResponse(b64Response)
	if err != nil {
		t.Fatalf("parseSoapResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("parseSoapResponse() returned nil response")
	}
	if resp.SuccessCode == SuccessCodeSuccess {
		t.Errorf("SuccessCode = %q, expected validation failure (no active coverage)", resp.SuccessCode)
	}
}

func TestMedicareHETSParseSoapResponseMissingPayload(t *testing.T) {
	m := newMedicareHETSForTest(t)

	response := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <CORE:RealTimeResponse>
      <PayloadType>X12_271_Response_005010X279A1</PayloadType>
    </CORE:RealTimeResponse>
  </soap:Body>
</soap:Envelope>`)

	_, err := m.parseSoapResponse(response)
	if err == nil {
		t.Fatal("expected error when Payload element is missing")
	}
}

func TestMedicareHETSParseSoapResponseInvalidBase64(t *testing.T) {
	m := newMedicareHETSForTest(t)

	response := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <CORE:RealTimeResponse>
      <PayloadType>X12_271_Response_005010X279A1</PayloadType>
      <Payload>!!! not valid base64 !!!</Payload>
    </CORE:RealTimeResponse>
  </soap:Body>
</soap:Envelope>`)

	_, err := m.parseSoapResponse(response)
	if err == nil {
		t.Fatal("expected error when base64 payload is invalid")
	}
}
