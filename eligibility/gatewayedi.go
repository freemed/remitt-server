package eligibility

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/freemed/remitt-server/crypto"
	"github.com/freemed/remitt-server/model"
)

// Plugin identifiers for GatewayEDI eligibility checker.
const (
	GatewayEDIEligibilityClass   = "org.remitt.plugin.eligibility.GatewayEDIEligibility"
	GatewayEDIEligibilityVersion = "0.1"
	GatewayEDIEligibilityKeyName = "GatewayEDI"

	// gatewayEdiServiceURIOption is the tUserConfig option that holds the
	// GatewayEDI SOAP endpoint URL.
	gatewayEdiServiceURIOption = "gatewayEdiServiceUri"

	// gatewayEdiSoapTimeout is the HTTP timeout for the SOAP POST. It matches
	// the 30s timeout used by callback/soap.go (defaultTimeout) and stedi.go
	// (StediTimeout).
	gatewayEdiSoapTimeout = 30 * time.Second

	// gatewayEdiSoapAction follows the SOAPAction convention used by
	// callback/soap.go: "<target namespace>/<SOAP body element>", taken from
	// the envelope built by buildSoapEnvelope.
	gatewayEdiSoapAction = "urn:remitt:eligibility/eligibilityRequest"

	// gatewayEdiMaxResponseBytes bounds how much of the SOAP response is read
	// (1 MiB), matching the limit used by optum.go.
	gatewayEdiMaxResponseBytes = 1 << 20
)

// GatewayEDI response element names.
//
// These are the documented element names of the GatewayEDI SOAP response, as
// declared by the sibling Java 0.5.x reference implementation's Axis-generated
// stubs (WebServices/GatewayEDI/WSEligibilityResponse.java and
// WebServices/GatewayEDI/ValidationFailureCollection.java): every element is
// declared in the GatewayEDI.WebServices namespace.
const (
	// gatewayEdiWebServicesNamespace is the target namespace of the
	// GatewayEDI WebServices schema.
	gatewayEdiWebServicesNamespace = "GatewayEDI.WebServices"

	// gatewayEdiSuccessCodeElement carries the SuccessCode enum value, e.g.
	// "Success" or "PayerTimeout". The Java plugin reads it as
	// response.getSuccessCode() and compares it case-insensitively.
	gatewayEdiSuccessCodeElement = "SuccessCode"

	// gatewayEdiExtraProcessingInfoElement carries the
	// ValidationFailureCollection whose AllMessages the Java plugin copies
	// into EligibilityResponse.messages.
	gatewayEdiExtraProcessingInfoElement = "ExtraProcessingInfo"

	// gatewayEdiAllMessagesElement carries the message strings inside
	// ExtraProcessingInfo; its item element is named "string".
	gatewayEdiAllMessagesElement = "AllMessages"

	// gatewayEdiRawResponseElement carries ResponseAsRawString, the value the
	// Java plugin stores as EligibilityResponse.rawResponse.
	gatewayEdiRawResponseElement = "ResponseAsRawString"
)

// gatewayEdiSuccessCodeMapping is the exact (Status, SuccessCode) pair the
// Java 0.5.x GatewayEDIEligibility plugin reports for a GatewayEDI SuccessCode
// value.
type gatewayEdiSuccessCodeMapping struct {
	Status      string
	SuccessCode string
}

// gatewayEdiSuccessCodeMappings maps the lower-cased SuccessCode element text
// to the pair reported by GatewayEDIEligibility.checkEligibility:
//
//	EligibilitySuccessCode is resolved first by equalsIgnoreCase
//	(GatewayEDIEligibility.java:161 and the private getSuccessCode helper at
//	lines 231-257, which falls through to SYSTEM_ERROR at line 256), and the
//	EligibilityStatus is then chosen by the SuccessCode enum identity checks at
//	lines 170-206, which fall through to SERVER_ERROR at line 205.
//
// Note that ProductRequired is a recognised SuccessCode (line 253) but has no
// branch in the status block, so the Java reports it as SERVER_ERROR /
// PRODUCT_REQUIRED.
var gatewayEdiSuccessCodeMappings = map[string]gatewayEdiSuccessCodeMapping{
	"success":                    {Status: StatusOK, SuccessCode: SuccessCodeSuccess},                     // Java:170-173
	"validationfailure":          {Status: StatusBad, SuccessCode: SuccessCodeValidationFailure},          // Java:175-178
	"payerenrollmentrequired":    {Status: StatusBad, SuccessCode: SuccessCodePayerEnrollmentRequired},    // Java:180-183
	"payernotsupported":          {Status: StatusServerError, SuccessCode: SuccessCodePayerNotSupported},  // Java:185-188
	"payertimeout":               {Status: StatusServerError, SuccessCode: SuccessCodePayerTimeout},       // Java:190-193
	"providerenrollmentrequired": {Status: StatusBad, SuccessCode: SuccessCodeProviderEnrollmentRequired}, // Java:195-198
	"systemerror":                {Status: StatusServerError, SuccessCode: SuccessCodeSystemError},        // Java:200-203
	"productrequired":            {Status: StatusServerError, SuccessCode: SuccessCodeProductRequired},    // Java:205 (no status branch)
}

// GatewayEDIEligibility implements the EligibilityChecker interface for
// the GatewayEDI SOAP-based eligibility service.
//
// Flow: values → buildSoapEnvelope → PGP encrypt with user's GatewayEDI
// public key → SOAP HTTP POST to gatewayEdiServiceUri → PGP decrypt the
// response with the user's private key if it is encrypted → parse SOAP
// response → EligibilityResponse.
//
// The response contract follows the Java 0.5.x reference implementation
// (org.remitt.plugin.eligibility.GatewayEDIEligibility): the gateway's
// SuccessCode element decides the outcome, its value is matched
// case-insensitively against the EligibilitySuccessCode enum names, and the
// resulting (Status, SuccessCode) pair is fixed by the contract. Every
// post-request failure (non-2xx HTTP status, undecryptable body, body with no
// recognised SuccessCode) is reported as Status StatusServerError /
// SuccessCode SuccessCodeSystemError, exactly as the Java falls through to
// SERVER_ERROR at line 205 and to SYSTEM_ERROR at line 256. This plugin never
// reports StatusOK / SuccessCodeSuccess unless an explicit SuccessCode of
// "Success" was parsed. Failures that happen before the request is sent
// (empty service URI, unbuildable request, transport error) are Go errors.
type GatewayEDIEligibility struct {
	ctx        context.Context
	httpClient *http.Client
}

func init() {
	RegisterChecker(GatewayEDIEligibilityClass, func() EligibilityChecker {
		return &GatewayEDIEligibility{}
	})
}

// buildSoapEnvelope creates a SOAP request envelope containing the given
// key-value pairs as elements in the eligibility request body.
func (g *GatewayEDIEligibility) buildSoapEnvelope(values map[string]string) ([]byte, error) {
	const soapTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Header/>
  <soapenv:Body>
    <eligibilityRequest xmlns="urn:remitt:eligibility">
      {{range $key, $value := .}}<{{$key}}>{{$value}}</{{$key}}>
      {{end}}    </eligibilityRequest>
  </soapenv:Body>
</soapenv:Envelope>`

	tmpl, err := template.New("soap").Parse(soapTmpl)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: parse soap template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, values); err != nil {
		return nil, fmt.Errorf("gatewayedi: execute soap template: %w", err)
	}

	return buf.Bytes(), nil
}

// CheckEligibility runs the GatewayEDI eligibility check: it builds the SOAP
// envelope, PGP-encrypts it with the user's GatewayEDI public key, POSTs the
// encrypted payload to the configured gatewayEdiServiceUri, and interprets the
// (PGP-encrypted, if the gateway encrypts it) SOAP response.
func (g *GatewayEDIEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	// Build the SOAP request envelope.
	envelope, err := g.buildSoapEnvelope(values)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: build envelope: %w", err)
	}

	// Retrieve the user's GatewayEDI public key from the keyring.
	key, err := model.GetKeyringEntry(userName, GatewayEDIEligibilityKeyName)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: keyring entry '%s' not found for user '%s': %w",
			GatewayEDIEligibilityKeyName, userName, err)
	}
	if len(key.PublicKey) == 0 {
		return nil, fmt.Errorf("gatewayedi: keyring entry '%s' for user '%s' has no public key",
			GatewayEDIEligibilityKeyName, userName)
	}

	// PGP-encrypt the SOAP request with the user's public key.
	encryptedPayload, err := crypto.EncryptPGP(envelope, key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: pgp encrypt: %w", err)
	}

	// Retrieve the service URI from user configuration.
	configs, err := model.GetConfigValues(userName)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: get config values: %w", err)
	}

	serviceUri := ""
	for _, cfg := range configs {
		if cfg.Option == gatewayEdiServiceURIOption {
			serviceUri = cfg.Value
			break
		}
	}

	// Without an endpoint there is nothing to contact: fail loudly rather than
	// reporting an eligibility result that was never obtained.
	if strings.TrimSpace(serviceUri) == "" {
		return nil, fmt.Errorf("gatewayedi: %s is not configured for user '%s'",
			gatewayEdiServiceURIOption, userName)
	}

	return g.postSoapRequest(serviceUri, encryptedPayload, key.PrivateKey, g.httpClient)
}

// postSoapRequest POSTs an already-PGP-encrypted SOAP payload to serviceUri
// and interprets the response.
//
// The resolved service URI, payload and key material are passed in, and no
// database access happens here, so the complete HTTP + PGP-response handling
// path is unit-testable without a keyring or tUserConfig.
//
// Failures that occur before the request is sent (empty URI, unbuildable
// request, transport error) are returned as Go errors. Failures that occur
// after the request was sent (non-2xx status, undecryptable body, body that is
// not a recognisable SOAP eligibility result) are returned as a response with
// Status StatusServerError and SuccessCode SuccessCodeSystemError, so callers
// that only inspect the response still cannot mistake them for success.
func (g *GatewayEDIEligibility) postSoapRequest(serviceUri string, payload, privateKey []byte, client *http.Client) (*EligibilityResponse, error) {
	if strings.TrimSpace(serviceUri) == "" {
		return nil, fmt.Errorf("gatewayedi: %s is not configured", gatewayEdiServiceURIOption)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("gatewayedi: refusing to POST an empty SOAP payload")
	}

	if client == nil {
		client = &http.Client{Timeout: gatewayEdiSoapTimeout}
	}

	ctx := g.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serviceUri, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: create soap request: %w", err)
	}
	// Headers follow the convention established by callback/soap.go.
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", gatewayEdiSoapAction)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: soap post: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, gatewayEdiMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: read soap response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := gatewayEdiSnippet(body)
		if snippet == "" {
			snippet = "<empty body>"
		}
		return gatewayEdiErrorResponse("GatewayEDI SOAP endpoint returned HTTP %d: %s",
			resp.StatusCode, snippet), nil
	}

	// The gateway PGP-encrypts its responses; decrypt them with the user's
	// private key exactly as the GatewayEDI scooper does for downloaded files.
	// crypto.IsPGPEncrypted only recognises ASCII-armored messages, while
	// crypto.EncryptPGP (the counterpart used for the request) emits binary
	// OpenPGP, so a body that is not XML is also treated as an encrypted
	// response and offered to the decryptor.
	plaintext := body
	if crypto.IsPGPEncrypted(body) || (len(privateKey) > 0 && !gatewayEdiLooksLikeXML(body)) {
		if len(privateKey) == 0 {
			return gatewayEdiErrorResponse("GatewayEDI response is PGP-encrypted but keyring entry '%s' has no private key",
				GatewayEDIEligibilityKeyName), nil
		}
		decrypted, decErr := crypto.DecryptPGP(body, privateKey)
		if decErr != nil {
			return gatewayEdiErrorResponse("GatewayEDI response is not XML and could not be PGP-decrypted: %v (body: %s)",
				decErr, gatewayEdiSnippet(body)), nil
		}
		plaintext = decrypted
	}

	return g.parseSoapEligibilityResponse(plaintext), nil
}

// parseSoapEligibilityResponse converts a decrypted SOAP response body into an
// EligibilityResponse, following the Java 0.5.x contract:
//
//   - a SOAP Fault (faultcode / faultstring) is always an error;
//   - the SuccessCode element (GatewayEDI.WebServices namespace) is matched
//     case-insensitively against the eight EligibilitySuccessCode enum names
//     and mapped through gatewayEdiSuccessCodeMappings;
//   - messages come from the ExtraProcessingInfo container
//     (AllMessages/string), which is what the Java copies out of
//     response.getExtraProcessingInfo().getAllMessages();
//   - RawResponse carries the ResponseAsRawString element when the response
//     has one (as the Java does) and otherwise the decrypted body verbatim.
//
// A body with no SuccessCode element, or one whose value is not a recognised
// enum name, is reported as StatusServerError / SuccessCodeSystemError: this
// plugin never reports success without an explicit, recognised SuccessCode.
func (g *GatewayEDIEligibility) parseSoapEligibilityResponse(body []byte) *EligibilityResponse {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return gatewayEdiErrorResponse("unrecognised GatewayEDI response format: empty response body (no %s element)",
			gatewayEdiSuccessCodeElement)
	}

	var doc gatewayEdiXMLNode
	if err := xml.Unmarshal(trimmed, &doc); err != nil {
		return gatewayEdiErrorResponse("unrecognised GatewayEDI response format: response is not valid XML (%v) (body: %s)",
			err, gatewayEdiSnippet(trimmed))
	}

	// A SOAP Fault is an explicit gateway-side failure.
	if fault := doc.find("Fault"); fault != nil {
		var parts []string
		for _, name := range []string{"faultcode", "faultstring"} {
			if n := fault.find(name); n != nil {
				if v := strings.TrimSpace(n.Value); v != "" {
					parts = append(parts, v)
				}
			}
		}
		detail := strings.Join(parts, " ")
		if detail == "" {
			detail = gatewayEdiSnippet(trimmed)
		}
		return gatewayEdiErrorResponse("GatewayEDI SOAP fault: %s", detail)
	}

	// Search the SOAP body when present, otherwise the whole document.
	scope := &doc
	if bodyNode := doc.find("Body"); bodyNode != nil {
		scope = bodyNode
	}

	resp := &EligibilityResponse{
		RawResponse: gatewayEdiRawResponse(scope, trimmed),
	}

	// The Java reads response.getSuccessCode() and switches on the enum; an
	// absent or unrecognised value leaves the success code at SYSTEM_ERROR and
	// the status at the SERVER_ERROR default.
	code := ""
	if codeNode := gatewayEdiFindSuccessCode(scope); codeNode != nil {
		code = strings.TrimSpace(codeNode.Value)
	}

	mapped, recognised := gatewayEdiSuccessCodeMappings[strings.ToLower(code)]
	if !recognised {
		var reason string
		if code == "" {
			reason = fmt.Sprintf("unrecognised GatewayEDI response format: no %s element found (body: %s)",
				gatewayEdiSuccessCodeElement, gatewayEdiSnippet(trimmed))
		} else {
			reason = fmt.Sprintf("unrecognised GatewayEDI %s value %q", gatewayEdiSuccessCodeElement, code)
		}
		return gatewayEdiErrorResponseWithRaw(resp.RawResponse, reason, gatewayEdiCollectMessages(scope))
	}

	resp.Status = mapped.Status
	resp.SuccessCode = mapped.SuccessCode
	if messages := gatewayEdiCollectMessages(scope); len(messages) > 0 {
		resp.Messages = messages
	}
	return resp
}

// gatewayEdiFindSuccessCode returns the SuccessCode element to interpret. The
// GatewayEDI.WebServices namespace declared by the Java stubs is preferred,
// but a namespace-agnostic local-name match is accepted as a fallback so a
// response whose gateway omits the namespace is still understood.
func gatewayEdiFindSuccessCode(scope *gatewayEdiXMLNode) *gatewayEdiXMLNode {
	var exact, any *gatewayEdiXMLNode
	scope.walk(func(n *gatewayEdiXMLNode) {
		if !strings.EqualFold(n.XMLName.Local, gatewayEdiSuccessCodeElement) {
			return
		}
		if any == nil {
			any = n
		}
		if exact == nil && n.XMLName.Space == gatewayEdiWebServicesNamespace {
			exact = n
		}
	})
	if exact != nil {
		return exact
	}
	return any
}

// gatewayEdiCollectMessages gathers the human-readable processing messages from
// a SOAP response scope.
//
// The Java copies response.getExtraProcessingInfo().getAllMessages() into
// EligibilityResponse.messages (GatewayEDIEligibility.java:164-168), so the
// ExtraProcessingInfo / AllMessages / string container is the primary source.
// Message elements elsewhere in the body are collected as a fallback, because
// the Java wraps that read in a try/catch and simply leaves the messages unset
// when the container is absent.
func gatewayEdiCollectMessages(scope *gatewayEdiXMLNode) []string {
	var messages []string

	if epi := scope.find(gatewayEdiExtraProcessingInfoElement); epi != nil {
		epi.walk(func(n *gatewayEdiXMLNode) {
			if !strings.EqualFold(n.XMLName.Local, gatewayEdiAllMessagesElement) {
				return
			}
			for i := range n.Children {
				if v := strings.TrimSpace(n.Children[i].Value); v != "" {
					messages = append(messages, v)
				}
			}
		})
		// A container without the AllMessages wrapper still carries its
		// messages as direct children.
		if len(messages) == 0 {
			for i := range epi.Children {
				if v := strings.TrimSpace(epi.Children[i].Value); v != "" {
					messages = append(messages, v)
				}
			}
		}
	}

	if len(messages) == 0 {
		scope.walk(func(n *gatewayEdiXMLNode) {
			switch strings.ToLower(n.XMLName.Local) {
			case "message", "description":
				if v := strings.TrimSpace(n.Value); v != "" {
					messages = append(messages, v)
				}
			}
		})
	}

	return messages
}

// gatewayEdiRawResponse returns the value for EligibilityResponse.RawResponse:
// the ResponseAsRawString element when the response carries one (this is the
// field the Java stores at GatewayEDIEligibility.java:158-160), otherwise the
// decrypted response body verbatim.
func gatewayEdiRawResponse(scope *gatewayEdiXMLNode, body []byte) string {
	if n := scope.find(gatewayEdiRawResponseElement); n != nil {
		if v := strings.TrimSpace(n.Value); v != "" {
			return v
		}
	}
	return string(body)
}

// gatewayEdiErrorResponse builds a non-OK eligibility response for a failure
// that occurred after the request was sent. The pair is the one the Java
// reports when it cannot interpret the response: Status SERVER_ERROR
// (GatewayEDIEligibility.java:205) with SuccessCode SYSTEM_ERROR
// (the getSuccessCode fall-through at line 256).
func gatewayEdiErrorResponse(format string, args ...any) *EligibilityResponse {
	return gatewayEdiErrorResponseWithRaw("", fmt.Sprintf(format, args...), nil)
}

// gatewayEdiErrorResponseWithRaw is gatewayEdiErrorResponse with an
// already-resolved RawResponse and additional messages preserved.
func gatewayEdiErrorResponseWithRaw(rawResponse, reason string, extra []string) *EligibilityResponse {
	messages := make([]string, 0, len(extra)+1)
	messages = append(messages, reason)
	messages = append(messages, extra...)

	return &EligibilityResponse{
		Status:      StatusServerError,
		SuccessCode: SuccessCodeSystemError,
		RawResponse: rawResponse,
		Messages:    messages,
	}
}

// gatewayEdiSnippet renders a bounded, single-line excerpt of a response body
// for error messages.
func gatewayEdiSnippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	const maxLen = 200
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}

// gatewayEdiLooksLikeXML reports whether data plausibly begins an XML document.
func gatewayEdiLooksLikeXML(data []byte) bool {
	s := bytes.TrimSpace(data)
	s = bytes.TrimPrefix(s, []byte("\xef\xbb\xbf"))
	s = bytes.TrimSpace(s)
	return len(s) > 0 && s[0] == '<'
}

// gatewayEdiXMLNode is a generic XML tree used to inspect a SOAP response
// without assuming a payer-specific schema.
type gatewayEdiXMLNode struct {
	XMLName  xml.Name
	Value    string              `xml:",chardata"`
	Children []gatewayEdiXMLNode `xml:",any"`
}

// walk calls fn for every node in the tree rooted at n, pre-order.
func (n *gatewayEdiXMLNode) walk(fn func(*gatewayEdiXMLNode)) {
	fn(n)
	for i := range n.Children {
		n.Children[i].walk(fn)
	}
}

// find returns the first node (pre-order) whose local name matches
// case-insensitively, or nil.
func (n *gatewayEdiXMLNode) find(local string) *gatewayEdiXMLNode {
	var found *gatewayEdiXMLNode
	n.walk(func(c *gatewayEdiXMLNode) {
		if found == nil && strings.EqualFold(c.XMLName.Local, local) {
			found = c
		}
	})
	return found
}

// GetPluginName returns the Java-style dotted class name of this plugin.
func (g *GatewayEDIEligibility) GetPluginName() string {
	return GatewayEDIEligibilityClass
}

// GetPluginVersion returns the plugin version.
func (g *GatewayEDIEligibility) GetPluginVersion() string {
	return GatewayEDIEligibilityVersion
}

// GetPluginConfigurationOptions returns the names of user-configurable
// options required by this plugin.
func (g *GatewayEDIEligibility) GetPluginConfigurationOptions() []string {
	return []string{
		"gatewayEdiUsername",
		"gatewayEdiPassword",
		"gatewayEdiServiceUri",
	}
}

// SetContext stores the execution context for use by this plugin.
func (g *GatewayEDIEligibility) SetContext(ctx context.Context) error {
	g.ctx = ctx
	return nil
}
