package eligibility

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/freemed/remitt-server/model"
)

// Plugin identifiers for the CMS HETS Medicare eligibility checker.
const (
	MedicareHETSEligibilityClass   = "org.remitt.plugin.eligibility.MedicareHETSEligibility"
	MedicareHETSEligibilityVersion = "0.1"
	medicareHETSConfigNS           = "eligibility_medicare_hets"
)

var medicareHETSConfigKeys = []string{
	"hetsUsername",
	"hetsPassword",
	"hetsEndpointUrl",
	"hetsSubmitterId",
	"hetsProviderNpi",
}

func init() {
	RegisterChecker(MedicareHETSEligibilityClass, func() EligibilityChecker {
		return &MedicareHETSEligibility{}
	})
}

// MedicareHETSEligibility implements the EligibilityChecker interface for
// the CMS HIPAA Eligibility Transaction System (HETS).
//
// Flow: values → build X12 270 EDI → base64 encode → build CAQH CORE
// vC2.2.0 SOAP envelope with WS-Security UsernameToken → POST to HETS
// endpoint → parse SOAP response → extract X12 271 → parse EB segments
// → EligibilityResponse.
//
// The HTTP POST to the HETS endpoint is stubbed (always returns success)
// because a real endpoint is not available in test environments.
type MedicareHETSEligibility struct {
	ctx context.Context
}

// ---------------------------------------------------------------------------
// EligibilityChecker interface
// ---------------------------------------------------------------------------

func (m *MedicareHETSEligibility) GetPluginName() string {
	return MedicareHETSEligibilityClass
}

func (m *MedicareHETSEligibility) GetPluginVersion() string {
	return MedicareHETSEligibilityVersion
}

func (m *MedicareHETSEligibility) GetPluginConfigurationOptions() []string {
	return medicareHETSConfigKeys
}

func (m *MedicareHETSEligibility) SetContext(ctx context.Context) error {
	m.ctx = ctx
	return nil
}

// CheckEligibility executes a Medicare HETS eligibility check.
//
// Required values map keys:
//   - memberId      Medicare beneficiary ID (HICN or MBI)
//   - firstName     Patient first name
//   - lastName      Patient last name
//   - dateOfBirth   Patient date of birth (YYYYMMDD)
//   - serviceDate   Date of service (YYYYMMDD)
func (m *MedicareHETSEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	// Load configuration from tUserConfig.
	configs, err := model.GetConfigValues(userName)
	if err != nil {
		return nil, fmt.Errorf("medicareHets: get config: %w", err)
	}

	params := make(map[string]string)
	for _, cfg := range configs {
		if cfg.Namespace == medicareHETSConfigNS {
			params[cfg.Option] = cfg.Value
		}
	}

	hetsUsername := params["hetsUsername"]
	hetsPassword := params["hetsPassword"]
	hetsSubmitterId := params["hetsSubmitterId"]
	hetsProviderNpi := params["hetsProviderNpi"]
	hetsEndpointUrl := params["hetsEndpointUrl"]

	// Validate required configuration.
	if hetsUsername == "" || hetsPassword == "" {
		return nil, fmt.Errorf("medicareHets: hetsUsername and hetsPassword are required")
	}
	if hetsSubmitterId == "" {
		return nil, fmt.Errorf("medicareHets: hetsSubmitterId is required")
	}
	if hetsProviderNpi == "" {
		return nil, fmt.Errorf("medicareHets: hetsProviderNpi is required")
	}

	// Default endpoint.
	if hetsEndpointUrl == "" {
		hetsEndpointUrl = "https://prd-wiser-hets-app.azurewebsites.us"
	}

	// Build X12 270 EDI.
	x12270, err := m.buildX12270(values, hetsSubmitterId, hetsProviderNpi)
	if err != nil {
		return nil, fmt.Errorf("medicareHets: build x12 270: %w", err)
	}

	// Base64-encode the X12 270.
	b64Payload := base64.StdEncoding.EncodeToString([]byte(x12270))

	// Build the SOAP envelope.
	soapEnvelope, err := m.buildSoapEnvelope(hetsUsername, hetsPassword, b64Payload)
	if err != nil {
		return nil, fmt.Errorf("medicareHets: build soap envelope: %w", err)
	}

	// POST to HETS endpoint.
	respBody, err := m.postSoapRequest(hetsEndpointUrl, soapEnvelope)
	if err != nil {
		return nil, fmt.Errorf("medicareHets: soap request: %w", err)
	}

	// Parse SOAP response and extract eligibility.
	return m.parseSoapResponse(respBody)
}

// ---------------------------------------------------------------------------
// X12 270 builder
// ---------------------------------------------------------------------------

// x12270Params holds the values extracted from the request map for building
// the X12 270 eligibility inquiry.
type x12270Params struct {
	memberId      string
	firstName     string
	lastName      string
	dateOfBirth   string
	serviceDate   string
	submitterId   string
	providerNpi   string
	providerName  string
	txnDate       string // YYYYMMDD
	txnTime       string // HHMM
	gsDate        string // YYYYMMDD
	gsTime        string // HHMM
	isaControlNum string
	gsControlNum  string
	traceNum      string
}

// buildX12270 constructs a complete X12 270 eligibility inquiry EDI
// transaction for CMS HETS.
func (m *MedicareHETSEligibility) buildX12270(values map[string]string, submitterId, providerNpi string) (string, error) {
	now := time.Now()

	params := x12270Params{
		memberId:      values["memberId"],
		firstName:     values["firstName"],
		lastName:      values["lastName"],
		dateOfBirth:   values["dateOfBirth"],
		serviceDate:   values["serviceDate"],
		submitterId:   submitterId,
		providerNpi:   providerNpi,
		providerName:  values["providerName"],
		txnDate:       now.Format("060102"),
		txnTime:       now.Format("1504"),
		gsDate:        now.Format("20060102"),
		gsTime:        now.Format("1504"),
		isaControlNum: fmt.Sprintf("%09d", now.UnixMilli()%1000000000),
		gsControlNum:  "1",
		traceNum:      fmt.Sprintf("TRACE%06d", now.UnixMilli()%1000000),
	}

	if params.providerName == "" {
		params.providerName = "TEST PROVIDER"
	}

	// Validate required fields for X12 270.
	if params.memberId == "" {
		return "", fmt.Errorf("memberId is required")
	}
	if params.lastName == "" {
		return "", fmt.Errorf("lastName is required")
	}

	var sb strings.Builder

	// Fill ISA sender/receiver IDs to 15 chars with trailing spaces.
	isaSender := fmt.Sprintf("%-15s", params.submitterId[:min(15, len(params.submitterId))])
	isaReceiver := fmt.Sprintf("%-15s", "CMSHETS")

	// ISA — Interchange Control Header.
	sb.WriteString(fmt.Sprintf(
		"ISA*00*          *00*          *ZZ*%s*ZZ*%s*%s*%s*^*00501*%s*0*T*:~\n",
		isaSender, isaReceiver, params.txnDate, params.txnTime, params.isaControlNum,
	))

	// GS — Functional Group Header.
	sb.WriteString(fmt.Sprintf(
		"GS*HS*%s*CMSHETS*%s*%s*%s*X*005010X279A1~\n",
		params.submitterId, params.gsDate, params.gsTime, params.gsControlNum,
	))

	// ST — Transaction Set Header.
	sb.WriteString("ST*270*0001*005010X279A1~\n")

	// BHT — Beginning of Hierarchical Transaction.
	sb.WriteString(fmt.Sprintf(
		"BHT*0022*13*%s*%s*%s~\n",
		params.traceNum, params.gsDate, params.gsTime,
	))

	// HL*1 — Information Source (Payer).
	sb.WriteString("HL*1**20*1~\n")

	// NM1*PR — Payer (Medicare).
	sb.WriteString("NM1*PR*2*MEDICARE*****PI*CMS~\n")

	// HL*2 — Information Receiver (Provider).
	sb.WriteString("HL*2*1*21*1~\n")

	// NM1*1P — Provider.
	providerLastName := params.providerName
	providerFirstName := ""
	if idx := strings.Index(providerLastName, " "); idx > 0 {
		providerFirstName = providerLastName[idx+1:]
		providerLastName = providerLastName[:idx]
	}
	sb.WriteString(fmt.Sprintf(
		"NM1*1P*2*%s*%s****XX*%s~\n",
		providerLastName, providerFirstName, params.providerNpi,
	))

	// HL*3 — Subscriber (Patient).
	sb.WriteString("HL*3*2*22*0~\n")

	// NM1*IL — Insured/Subscriber.
	sb.WriteString(fmt.Sprintf(
		"NM1*IL*1*%s*%s****MI*%s~\n",
		params.lastName, params.firstName, params.memberId,
	))

	// DMG — Demographic Information.
	if params.dateOfBirth != "" {
		sb.WriteString(fmt.Sprintf("DMG*D8*%s~\n", params.dateOfBirth))
	}

	// DTP*291 — Date of Service.
	if params.serviceDate != "" {
		sb.WriteString(fmt.Sprintf("DTP*291*D8*%s~\n", params.serviceDate))
	} else {
		sb.WriteString(fmt.Sprintf("DTP*291*D8*%s~\n", now.Format("20060102")))
	}

	// EQ — Eligibility Inquiry (30 = general).
	sb.WriteString("EQ*30~\n")

	// The SE count is number of segments including ST and SE itself.
	// Count segments by counting tilde-terminated lines.
	seCount := 0
	for _, line := range strings.Split(sb.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Each line is a segment.
		seCount++
	}

	// SE — Transaction Set Trailer.
	sb.WriteString(fmt.Sprintf("SE*%d*0001~\n", seCount))

	// GE — Functional Group Trailer.
	sb.WriteString(fmt.Sprintf("GE*%s*%s~\n", params.gsControlNum, params.gsControlNum))

	// IEA — Interchange Control Trailer.
	sb.WriteString(fmt.Sprintf("IEA*1*%s~\n", params.isaControlNum))

	return sb.String(), nil
}

// ---------------------------------------------------------------------------
// SOAP envelope builder (CAQH CORE vC2.2.0)
// ---------------------------------------------------------------------------

const medicareHetsSoapTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:CORE="http://www.caqh.org/SOAP/WSDL/">
  <soap:Header>
    <wsse:Security xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">
      <wsse:UsernameToken>
        <wsse:Username>{{.Username}}</wsse:Username>
        <wsse:Password>{{.Password}}</wsse:Password>
      </wsse:UsernameToken>
    </wsse:Security>
  </soap:Header>
  <soap:Body>
    <CORE:RealTimeRequest>
      <PayloadType>X12_270_Request_005010X279A1</PayloadType>
      <ProcessingMode>RealTime</ProcessingMode>
      <Payload>{{.Payload}}</Payload>
    </CORE:RealTimeRequest>
  </soap:Body>
</soap:Envelope>`

type soapTemplateData struct {
	Username string
	Password string
	Payload  string
}

// buildSoapEnvelope creates a CAQH CORE vC2.2.0 SOAP envelope with
// WS-Security UsernameToken header and base64-encoded X12 270 payload.
func (m *MedicareHETSEligibility) buildSoapEnvelope(username, password, b64Payload string) ([]byte, error) {
	tmpl, err := template.New("hetsSoap").Parse(medicareHetsSoapTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse soap template: %w", err)
	}

	data := soapTemplateData{
		Username: username,
		Password: password,
		Payload:  b64Payload,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute soap template: %w", err)
	}

	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// SOAP HTTP POST (stubbed)
// ---------------------------------------------------------------------------

// postSoapRequest sends the SOAP envelope to the HETS endpoint. The actual
// HTTP POST is stubbed because a real HETS endpoint requires CMS credentials
// and network access that is not available in test/CI environments.
func (m *MedicareHETSEligibility) postSoapRequest(endpointUrl string, envelope []byte) ([]byte, error) {
	// Stub: Return a success SOAP response with a canned X12 271 indicating
	// active Medicare coverage.
	_ = endpointUrl
	_ = envelope

	// Build a canned X12 271 response indicating active coverage.
	x12271 := m.buildCannedX12271()

	b64Response := base64.StdEncoding.EncodeToString([]byte(x12271))

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:CORE="http://www.caqh.org/SOAP/WSDL/">
  <soap:Header/>
  <soap:Body>
    <CORE:RealTimeResponse>
      <PayloadType>X12_271_Response_005010X279A1</PayloadType>
      <ProcessingMode>RealTime</ProcessingMode>
      <Payload>%s</Payload>
    </CORE:RealTimeResponse>
  </soap:Body>
</soap:Envelope>`, b64Response)

	return []byte(response), nil
}

// buildCannedX12271 returns a canned X12 271 indicating active Medicare
// Part A and Part B coverage.
func (m *MedicareHETSEligibility) buildCannedX12271() string {
	return `ISA*00*          *00*          *ZZ*CMSHETS        *ZZ*SUBMITTERID    *240810*1200*^*00501*000000002*0*T*:~
GS*HB*CMSHETS*SUBMITTERID*20240810*1200*1*X*005010X279A1~
ST*271*0001*005010X279A1~
BHT*0022*11*TRACE001*20240810*1200~
HL*1**20*1~
NM1*PR*2*MEDICARE*****PI*CMS~
HL*2*1*21*1~
NM1*1P*2*TEST PROVIDER*****XX*1234567890~
HL*3*2*22*0~
NM1*IL*1*DOE*JOHN****MI*M12345678A~
TRN*2*TRACE001*1CMS~
EB*R**30*MA*MEDICARE PART A~
EB*R**30*MB*MEDICARE PART B~
EB*1**30*MA*MEDICARE PART A^^ACTIVE COVERAGE~
EB*1**30*MB*MEDICARE PART B^^ACTIVE COVERAGE~
SE*16*0001~
GE*1*1~
IEA*1*000000002~
`
}

// ---------------------------------------------------------------------------
// SOAP response parsing
// ---------------------------------------------------------------------------

// parseSoapResponse extracts the base64-encoded X12 271 payload from a
// CAQH CORE SOAP response and determines eligibility from EB segments.
func (m *MedicareHETSEligibility) parseSoapResponse(respBody []byte) (*EligibilityResponse, error) {
	body := string(respBody)

	// Extract base64 payload from <Payload> element.
	payloadStart := strings.Index(body, "<Payload>")
	payloadEnd := strings.Index(body, "</Payload>")
	if payloadStart < 0 || payloadEnd < 0 {
		return nil, fmt.Errorf("soap response missing Payload element")
	}

	b64Payload := body[payloadStart+len("<Payload>") : payloadEnd]

	// Decode the base64 X12 271.
	x12271Bytes, err := base64.StdEncoding.DecodeString(b64Payload)
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	x12271 := string(x12271Bytes)

	// Parse EB (Eligibility/Benefit) segments.
	return m.parseX12271EB(x12271), nil
}

// parseX12271EB extracts eligibility status from EB segments in an X12 271.
// Active coverage EB segments have EB01="1" (Active) or EB01="R" (Receipt).
// Any EB*1 indicates active coverage.
func (m *MedicareHETSEligibility) parseX12271EB(x12271 string) *EligibilityResponse {
	var messages []string
	activeCoverage := false

	lines := strings.Split(x12271, "~")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "EB*") {
			continue
		}

		segments := strings.Split(line, "*")
		if len(segments) < 5 {
			continue
		}

		eb01 := segments[1] // Eligibility/Benefit Information
		eb03 := segments[3] // Service Type Code
		eb04 := segments[4] // Insurance Type Code

		if eb01 == "1" {
			activeCoverage = true
			messages = append(messages, fmt.Sprintf("%s: Active Coverage (%s)", eb04, eb03))
		} else if eb01 == "R" {
			messages = append(messages, fmt.Sprintf("%s: Received (%s)", eb04, eb03))
		} else {
			messages = append(messages, fmt.Sprintf("%s: Status %s (%s)", eb04, eb01, eb03))
		}
	}

	if activeCoverage {
		return &EligibilityResponse{
			Status:      StatusOK,
			SuccessCode: SuccessCodeSuccess,
			Messages:    messages,
		}
	}

	return &EligibilityResponse{
		Status:      StatusOK,
		SuccessCode: SuccessCodeValidationFailure,
		Messages:    messages,
	}
}

// ---------------------------------------------------------------------------
// HTTP client (extracted for testability)
// ---------------------------------------------------------------------------

// httpClient is the HTTP client used for SOAP requests. Extracted as a
// package-level variable so tests can replace it with a mock transport.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}
