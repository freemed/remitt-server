package eligibility

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/freemed/remitt-server/model"
)

// ---------------------------------------------------------------------------
// THIS PLUGIN IS NOT IMPLEMENTED — read this before treating it as working.
// ---------------------------------------------------------------------------
//
// The CMS HETS (HIPAA Eligibility Transaction System) exchange is NOT
// implemented here. There is no live CMS endpoint wired up, the CAQH CORE
// vC2.2.0 / WS-Security exchange has never been exercised against CMS, and the
// deployments that ship this plugin have no CMS/HETS credentials at all. The
// Java 0.5.x reference tree (org.remitt.plugin.eligibility) has no HETS plugin
// either, so there is no reference implementation to port from.
//
// What this file does implement: request *shaping* (X12 270 build, CAQH CORE
// SOAP envelope build) and a genuine HTTP POST to the endpoint the user
// configured, so that an unreachable endpoint is reported as unreachable.
//
// What it deliberately does NOT implement: any stand-in for the CMS
// conversation. When the plugin cannot actually reach CMS — no credentials
// configured, no endpoint configured, endpoint unreachable, unusable HTTP
// response, or an unparseable body — it reports an explicit server-side
// failure, Status StatusServerError with SuccessCode SuccessCodeSystemError
// (the pair the sibling GatewayEDIEligibility plugin uses for a failure it
// cannot interpret), plus a message naming what is missing. It never reports
// StatusOK / SuccessCodeSuccess unless an actual X12 271 returned by the
// configured endpoint was parsed.
//
// History / warning to future maintainers: an earlier revision of this file
// had postSoapRequest() ignore the endpoint and return a hard-coded X12 271
// claiming active Medicare Part A/B coverage, so every eligibility job came
// back "active coverage" while nothing had been sent anywhere. That canned
// fixture has been deleted outright: no code path in this package can
// legitimately produce a 271 without a real CMS response, so it must never
// reappear as a success. A fabricated eligibility result is worse than an
// error — it is indistinguishable from a real one to the caller, the stored
// job response and the UI.

// Plugin identifiers for the CMS HETS Medicare eligibility checker.
const (
	MedicareHETSEligibilityClass   = "org.remitt.plugin.eligibility.MedicareHETSEligibility"
	MedicareHETSEligibilityVersion = "0.1"
	medicareHETSConfigNS           = "eligibility_medicare_hets"

	// medicareHetsTimeout bounds the SOAP POST, matching the 30s timeout used
	// by the sibling plugins (gatewayEdiSoapTimeout, StediTimeout).
	medicareHetsTimeout = 30 * time.Second

	// medicareHetsMaxResponseBytes bounds how much of the SOAP response is read
	// (1 MiB), matching optum.go and gatewayedi.go.
	medicareHetsMaxResponseBytes = 1 << 20

	// medicareHetsSoapAction is the SOAPAction header sent with the request.
	// It follows the "<target namespace>/<SOAP body element>" convention used
	// by callback/soap.go and gatewayedi.go, but it has never been validated
	// against CMS, because the real CMS exchange is not implemented.
	medicareHetsSoapAction = "urn:remitt:eligibility/hetsEligibilityRequest"
)

// The CMS production HETS endpoint is
// https://prd-wiser-hets-app.azurewebsites.us. It is deliberately NOT used as
// an implicit default any more: silently directing traffic at a URL this
// plugin has never successfully talked to turned "not configured" into a
// confusing runtime failure. With no hetsEndpointUrl the plugin now reports a
// configuration failure naming the missing option.

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
// Intended flow, of which only the first half exists: values → build X12 270
// EDI → base64 encode → build CAQH CORE vC2.2.0 SOAP envelope with
// WS-Security UsernameToken → POST to the configured HETS endpoint → parse
// SOAP response → extract X12 271 → parse EB segments → EligibilityResponse.
//
// See the package-top note in this file: the CMS side of that flow is not
// implemented, and every "cannot reach CMS" outcome is reported as
// StatusServerError / SuccessCodeSystemError rather than as eligibility.
type MedicareHETSEligibility struct {
	ctx context.Context

	// httpClient is the client used for the SOAP POST. It is a field (rather
	// than only a package-level variable) so tests and callers can inject a
	// client, as GatewayEDIEligibility does.
	httpClient *http.Client
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

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// medicareHetsConfig is the resolved HETS configuration for one check. It is a
// plain value so the whole decision path below can be exercised without a
// database (see checkEligibilityWithConfig).
type medicareHetsConfig struct {
	username    string
	password    string
	submitterID string
	providerNPI string
	endpointURL string
}

// missingCredentials returns the names of the credential options that are not
// configured. The names are the tUserConfig option names, so the message the
// user sees points straight at what has to be filled in.
func (c medicareHetsConfig) missingCredentials() []string {
	var missing []string
	if strings.TrimSpace(c.username) == "" {
		missing = append(missing, "hetsUsername")
	}
	if strings.TrimSpace(c.password) == "" {
		missing = append(missing, "hetsPassword")
	}
	if strings.TrimSpace(c.submitterID) == "" {
		missing = append(missing, "hetsSubmitterId")
	}
	if strings.TrimSpace(c.providerNPI) == "" {
		missing = append(missing, "hetsProviderNpi")
	}
	return missing
}

// CheckEligibility executes a Medicare HETS eligibility check.
//
// Required values map keys:
//   - memberId      Medicare beneficiary ID (HICN or MBI)
//   - firstName     Patient first name
//   - lastName      Patient last name
//   - dateOfBirth   Patient date of birth (YYYYMMDD)
//   - serviceDate   Date of service (YYYYMMDD)
//
// This method resolves the user's configuration and then delegates to
// checkEligibilityWithConfig, which holds the entire decision path. Every
// failure to actually reach CMS is returned as a non-OK EligibilityResponse
// (StatusServerError / SuccessCodeSystemError) *and* as a Go error with the
// same text, so a caller cannot mistake it for an eligibility result whichever
// of the two it inspects.
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

	return m.checkEligibilityWithConfig(userName, values, medicareHetsConfig{
		username:    params["hetsUsername"],
		password:    params["hetsPassword"],
		submitterID: params["hetsSubmitterId"],
		providerNPI: params["hetsProviderNpi"],
		endpointURL: params["hetsEndpointUrl"],
	}, m.httpClient)
}

// checkEligibilityWithConfig runs the eligibility check against an
// already-resolved configuration, with no database access, so the complete
// path — configuration validation, X12 270 build, SOAP envelope build, HTTP
// POST — is unit-testable.
//
// It returns a non-nil failure response together with a non-nil error for
// every "could not actually reach CMS" outcome. The response carries
// Status StatusServerError / SuccessCode SuccessCodeSystemError and a message
// naming the missing or broken configuration, which is what makes the failure
// visible in the stored job response; the error carries the same text so the
// task runner (task.EligibilityTask) records the job as failed. There is no
// input that makes this function report StatusOK / SuccessCodeSuccess without
// a parsed X12 271 from the configured endpoint.
func (m *MedicareHETSEligibility) checkEligibilityWithConfig(userName string, values map[string]string, cfg medicareHetsConfig, client *http.Client) (*EligibilityResponse, error) {
	// No credentials: there is nothing to authenticate with, so nothing can be
	// asked of CMS. Fail loudly, naming exactly which options are missing.
	if missing := cfg.missingCredentials(); len(missing) > 0 {
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: cannot check Medicare eligibility for user %q: HETS credentials are not configured (missing %s). "+
				"The real CMS HETS call is not implemented in this plugin, so no eligibility result can be reported for this request",
			userName, strings.Join(missing, ", ")))
	}

	// No endpoint: refuse to invent one. (See the note on the production URL
	// above: this plugin will not silently POST to a default CMS address.)
	if strings.TrimSpace(cfg.endpointURL) == "" {
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: cannot check Medicare eligibility for user %q: hetsEndpointUrl is not configured, "+
				"and this plugin no longer assumes a default CMS HETS endpoint. The real CMS HETS call is not implemented in this plugin",
			userName))
	}

	// Build X12 270 EDI.
	x12270, err := m.buildX12270(values, cfg.submitterID, cfg.providerNPI)
	if err != nil {
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: cannot build the X12 270 eligibility inquiry for user %q: %v", userName, err))
	}

	// Base64-encode the X12 270.
	b64Payload := base64.StdEncoding.EncodeToString([]byte(x12270))

	// Build the SOAP envelope.
	soapEnvelope, err := m.buildSoapEnvelope(cfg.username, cfg.password, b64Payload)
	if err != nil {
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: cannot build the CAQH CORE SOAP envelope for user %q: %v", userName, err))
	}

	// POST to the configured HETS endpoint. This is a real HTTP request: if the
	// endpoint is unreachable, the failure is reported as such below.
	return m.postSoapRequest(cfg.endpointURL, soapEnvelope, client)
}

// medicareHetsUnavailable builds the (response, error) pair returned for every
// failure to actually reach CMS. The pair is always returned together and with
// identical wording: the response is what a caller inspecting the payload sees
// (StatusStatusServerError / SuccessCodeSystemError, never OK/SUCCESS), the
// error is what a caller that only checks its error return sees. Neither can
// be read as a successful eligibility check.
func medicareHetsUnavailable(reason string) (*EligibilityResponse, error) {
	return &EligibilityResponse{
		Status:      StatusServerError,
		SuccessCode: SuccessCodeSystemError,
		Messages:    []string{reason},
	}, errors.New(reason)
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
//
// This is request shaping only: it does not mean the transaction can be sent
// to CMS (see the file-top note).
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
//
// Note (WS-Security): the UsernameToken here is a plain-text token. CMS HETS
// expects a signed, timestamped token over TLS; that handshake is part of the
// unimplemented CMS exchange, so this envelope shape has never been accepted
// by CMS and must not be read as a working request.
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
// SOAP HTTP POST
// ---------------------------------------------------------------------------

// postSoapRequest sends the SOAP envelope to the configured HETS endpoint over
// real HTTP and interprets whatever comes back.
//
// The CMS HETS call itself is not implemented (see the file-top note), so in
// practice this POST reaches either a misconfigured/unreachable address or
// something that is not CMS. Every outcome other than "a SOAP response
// containing a parseable X12 271" is reported as
// StatusServerError / SuccessCodeSystemError with an explanatory message — in
// particular this function never fabricates a 271, and there is no fixture
// standing in for a CMS response.
func (m *MedicareHETSEligibility) postSoapRequest(endpointUrl string, envelope []byte, client *http.Client) (*EligibilityResponse, error) {
	endpointUrl = strings.TrimSpace(endpointUrl)
	if endpointUrl == "" {
		return medicareHetsUnavailable(
			"medicareHets: hetsEndpointUrl is not configured; there is no CMS HETS endpoint to contact")
	}
	if len(envelope) == 0 {
		return medicareHetsUnavailable(
			"medicareHets: refusing to POST an empty SOAP envelope to the CMS HETS endpoint")
	}

	if client == nil {
		client = httpClient
	}
	if client == nil {
		client = &http.Client{Timeout: medicareHetsTimeout}
	}

	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointUrl, bytes.NewReader(envelope))
	if err != nil {
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: cannot build the SOAP request for CMS HETS endpoint %s: %v", endpointUrl, err))
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", medicareHetsSoapAction)

	resp, err := client.Do(req)
	if err != nil {
		// Unreachable endpoint (connection refused, DNS failure, TLS failure,
		// timeout): report it as a failure. Never fall back to a canned result.
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: cannot reach the CMS HETS endpoint %s: %v", endpointUrl, err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, medicareHetsMaxResponseBytes))
	if err != nil {
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: cannot read the CMS HETS response from %s: %v", endpointUrl, err))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := medicareHetsSnippet(body)
		if snippet == "" {
			snippet = "<empty body>"
		}
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: CMS HETS endpoint %s returned HTTP %d: %s", endpointUrl, resp.StatusCode, snippet))
	}

	parsed, err := m.parseSoapResponse(body)
	if err != nil {
		return medicareHetsUnavailable(fmt.Sprintf(
			"medicareHets: cannot interpret the response from CMS HETS endpoint %s: %v", endpointUrl, err))
	}
	parsed.RawResponse = string(body)
	return parsed, nil
}

// medicareHetsSnippet renders a bounded, single-line excerpt of a response
// body for error messages.
func medicareHetsSnippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	const maxLen = 200
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}

// ---------------------------------------------------------------------------
// SOAP response parsing
// ---------------------------------------------------------------------------

// parseSoapResponse extracts the base64-encoded X12 271 payload from a
// CAQH CORE SOAP response and determines eligibility from EB segments.
//
// This is the only path in the package that can report StatusOK /
// SuccessCodeSuccess, and it can only do so from a 271 that the configured
// endpoint actually returned. It is unreachable in practice because the CMS
// call is not implemented; it is kept so that a future implementation parses
// real responses rather than inventing them.
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

// httpClient is the default HTTP client used for the SOAP POST when neither
// the plugin instance nor the caller supplied one. Extracted as a
// package-level variable so tests can replace it with a mock transport.
var httpClient = &http.Client{
	Timeout: medicareHetsTimeout,
}
