package eligibility

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
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

	// gatewayEdiUsernameOption and gatewayEdiPasswordOption are the tUserConfig
	// options (tUserConfig.cOption) holding the GatewayEDI account credentials.
	// They are sent as HTTP Basic auth, which is what the Java 0.5.x plugin
	// does by setting Call.USERNAME_PROPERTY / Call.PASSWORD_PROPERTY on the
	// Axis stub (GatewayEDIEligibility.java:139-143).
	gatewayEdiUsernameOption = "gatewayEdiUsername"
	gatewayEdiPasswordOption = "gatewayEdiPassword"

	// gatewayEdiSoapTimeout is the HTTP timeout for the SOAP POST. It matches
	// the 30s timeout used by callback/soap.go (defaultTimeout) and stedi.go
	// (StediTimeout).
	gatewayEdiSoapTimeout = 30 * time.Second

	// gatewayEdiSoapAction is the SOAPAction the vendor WSDL declares for the
	// DoInquiry operation:
	//
	//	<wsdl:operation name="DoInquiry">
	//	  <soap:operation soapAction="GatewayEDI.WebServices/DoInquiry" style="document"/>
	//
	// (https://services.gatewayedi.com/eligibility/service.asmx?WSDL, binding
	// EligibilitySoap, port EligibilitySoap). SOAP 1.1 requires the SOAPAction
	// header for a document/literal call.
	gatewayEdiSoapAction = "GatewayEDI.WebServices/DoInquiry"

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

// GatewayEDI DoInquiry request element names, taken from the vendor WSDL at
// https://services.gatewayedi.com/eligibility/service.asmx?WSDL.
//
// The schema is declared elementFormDefault="qualified" with
// targetNamespace="GatewayEDI.WebServices", so every element of the request
// body lives in that namespace. The WSDL declares:
//
//	<s:element name="DoInquiry">
//	  <s:complexType><s:sequence>
//	    <s:element minOccurs="0" maxOccurs="1" name="Inquiry" type="tns:WSEligibilityInquiry" />
//	  </s:sequence></s:complexType>
//	</s:element>
//	<s:complexType name="WSEligibilityInquiry"><s:sequence>
//	  <s:element minOccurs="0" maxOccurs="1" name="Parameters" type="tns:ArrayOfMyNameValue" />
//	  <s:element minOccurs="1" maxOccurs="1" name="ResponseDataType" type="tns:WSResponseDataType" />
//	</s:sequence></s:complexType>
//	<s:complexType name="ArrayOfMyNameValue"><s:sequence>
//	  <s:element minOccurs="0" maxOccurs="unbounded" name="MyNameValue" nillable="true" type="tns:MyNameValue" />
//	</s:sequence></s:complexType>
//	<s:complexType name="MyNameValue"><s:sequence>
//	  <s:element minOccurs="0" maxOccurs="1" name="Name" type="s:string" />
//	  <s:element minOccurs="0" maxOccurs="1" name="Value" type="s:string" />
//	</s:sequence></s:complexType>
//
// The message DoInquirySoapIn carries the single part "parameters" bound to the
// element tns:DoInquiry, i.e. the request wrapper element is literally named
// "DoInquiry" (the portType operation is also named "DoInquiry"; the Java
// method on the generated stub is doInquiry(...)).
const (
	// gatewayEdiInquiryOperationElement is the request wrapper element of the
	// DoInquiry operation (WSDL element "DoInquiry", message DoInquirySoapIn).
	gatewayEdiInquiryOperationElement = "DoInquiry"

	// gatewayEdiInquiryContainerElement is the Inquiry child of the request
	// wrapper (WSDL element "Inquiry", type WSEligibilityInquiry).
	gatewayEdiInquiryContainerElement = "Inquiry"

	// gatewayEdiInquiryParametersElement is the array wrapper holding the
	// name/value pairs (WSDL element "Parameters", type ArrayOfMyNameValue).
	gatewayEdiInquiryParametersElement = "Parameters"

	// gatewayEdiInquiryMyNameValueElement is the repeated array item (WSDL
	// element "MyNameValue", type MyNameValue, unbounded).
	gatewayEdiInquiryMyNameValueElement = "MyNameValue"

	// gatewayEdiInquiryNameElement and gatewayEdiInquiryValueElement are the
	// two strings every MyNameValue carries.
	gatewayEdiInquiryNameElement  = "Name"
	gatewayEdiInquiryValueElement = "Value"

	// gatewayEdiInquiryResponseDataTypeElement selects the response
	// representation (WSDL element "ResponseDataType", type
	// WSResponseDataType, minOccurs="1").
	gatewayEdiInquiryResponseDataTypeElement = "ResponseDataType"

	// gatewayEdiResponseDataTypeXml is the WSResponseDataType enumeration value
	// the Java plugin asks for: inq.setResponseDataType(WSResponseDataType.Xml)
	// (GatewayEDIEligibility.java:147). The WSDL enumerates exactly "Xml" and
	// "RawPayerData".
	gatewayEdiResponseDataTypeXml = "Xml"

	// gatewayEdiSoapEnvelopeNamespace is the SOAP 1.1 envelope namespace; the
	// WSDL binds the service to SOAP 1.1 over HTTP
	// (soap:binding transport="http://schemas.xmlsoap.org/soap/http").
	gatewayEdiSoapEnvelopeNamespace = "http://schemas.xmlsoap.org/soap/envelope/"

	// gatewayEdiSoapPrefix and gatewayEdiWebServicesPrefix are the prefixes
	// used for the two namespaces of the request document. The gateway parses
	// by namespace, not by prefix.
	gatewayEdiSoapPrefix        = "soapenv"
	gatewayEdiWebServicesPrefix = "gw"
)

// gatewayEdiInquiryParameter is one name/value pair of the DoInquiry request:
// the key in the eligibility request's values map, and the GatewayEDI
// parameter Name it is sent under.
type gatewayEdiInquiryParameter struct {
	Key  string // key in the EligibilityChecker values map
	Name string // GatewayEDI parameter name (MyNameValue/Name)
}

// gatewayEdiInquiryParameters is the payer/provider/subscriber/patient payload
// of the DoInquiry request, in the exact order and under the exact GatewayEDI
// names the Java 0.5.x GatewayEDIEligibility builds with addNameValue
// (GatewayEDIEligibility.java:87-137). The keys are the EligibilityParameter
// enum values declared by org.remitt.prototype.EligibilityParameter (npi,
// insuranceId, insuredLastName, ... groupId), which is the vocabulary the
// eligibility request's values map uses.
//
// addNameValue only appends a pair when the caller supplied that parameter, so
// a key the request does not carry contributes no element to the body.
var gatewayEdiInquiryParameters = []gatewayEdiInquiryParameter{
	{Key: "npi", Name: "NPI"},                                         // Java:88-89
	{Key: "insuranceId", Name: "InsuranceNum"},                        // Java:90-91
	{Key: "insuredLastName", Name: "InsuredLastName"},                 // Java:92-94
	{Key: "insuredFirstName", Name: "InsuredFirstName"},               // Java:95-97
	{Key: "insuredDateOfBirth", Name: "InsuredDob"},                   // Java:98-100
	{Key: "insuredGender", Name: "InsuredGender"},                     // Java:101-103
	{Key: "insuredState", Name: "InsuredState"},                       // Java:104-106
	{Key: "insuredSsn", Name: "InsuredSsn"},                           // Java:107-109
	{Key: "dependentLastName", Name: "DependentLastName"},             // Java:110-112
	{Key: "dependentFirstName", Name: "DependentFirstName"},           // Java:113-117
	{Key: "dependentDateOfBirth", Name: "DependentDob"},               // Java:118-120
	{Key: "dependentGender", Name: "DependentGender"},                 // Java:121-123
	{Key: "dependentRelationship", Name: "DependentRelationshipCode"}, // Java:124-128
	{Key: "serviceType", Name: "ServiceTypeCode"},                     // Java:129-131
	{Key: "cardIssueDate", Name: "CardIssueDate"},                     // Java:132-134
	{Key: "groupId", Name: "GroupNumber"},                             // Java:135-137
}

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
// Flow: values → buildSoapEnvelope (SOAP 1.1 DoInquiry, plain XML) → SOAP HTTP
// POST to gatewayEdiServiceUri with HTTP Basic credentials → PGP decrypt the
// response with the user's private key if it is encrypted → parse SOAP
// response → EligibilityResponse.
//
// The request is not encrypted: the vendor's DoInquiry operation is a plain
// document/literal SOAP call authenticated with HTTP Basic, exactly as the
// Java 0.5.x plugin drives the Axis-generated EligibilitySoap stub
// (GatewayEDIEligibility.java:139-151). PGP is still honoured on the response
// side, where a gateway that returns an encrypted body can be decrypted with
// the user's GatewayEDI private key.
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

// gatewayEdiNameValue is one resolved MyNameValue pair of the request body.
type gatewayEdiNameValue struct {
	Name  string
	Value string
}

// gatewayEdiInquiryNameValues resolves the caller's eligibility values into the
// ordered MyNameValue list of the DoInquiry request, following the Java 0.5.x
// addNameValue behaviour: a parameter the request carries no value for is
// omitted from the body entirely. (The Java appends the pair whenever the map
// holds a non-null entry; here a present-but-blank value is dropped rather than
// sent as an empty element, which the gateway only rejects as a
// ValidationFailure.)
//
// Keys are matched exactly first, then case-insensitively, because the values
// map is caller-supplied (API payload or stored eligibility job).
func gatewayEdiInquiryNameValues(values map[string]string) []gatewayEdiNameValue {
	var pairs []gatewayEdiNameValue

	for _, p := range gatewayEdiInquiryParameters {
		v, ok := values[p.Key]
		if !ok {
			v, ok = gatewayEdiLookupFold(values, p.Key)
		}
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		pairs = append(pairs, gatewayEdiNameValue{Name: p.Name, Value: v})
	}

	return pairs
}

// gatewayEdiLookupFold finds key in values ignoring case, so a request that
// capitalises a parameter differently still reaches the gateway.
func gatewayEdiLookupFold(values map[string]string, key string) (string, bool) {
	for k, v := range values {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

// buildSoapEnvelope creates the SOAP 1.1 DoInquiry request document declared by
// the vendor WSDL: the envelope carries <DoInquiry><Inquiry>, the Inquiry holds
// the caller's payer/provider/subscriber/patient values as MyNameValue pairs
// inside <Parameters>, and ResponseDataType is Xml.
//
// Every element is namespace-qualified in GatewayEDI.WebServices
// (elementFormDefault="qualified" in the WSDL). Values are XML-escaped, so a
// value containing markup cannot break the document. No PGP encryption is
// applied to the request.
func (g *GatewayEDIEligibility) buildSoapEnvelope(values map[string]string) ([]byte, error) {
	pairs := gatewayEdiInquiryNameValues(values)

	gw := gatewayEdiWebServicesPrefix + ":"

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<` + gatewayEdiSoapPrefix + `:Envelope xmlns:` + gatewayEdiSoapPrefix + `="` + gatewayEdiSoapEnvelopeNamespace + `" xmlns:` + gatewayEdiWebServicesPrefix + `="` + gatewayEdiWebServicesNamespace + `">` + "\n")
	buf.WriteString(`  <` + gatewayEdiSoapPrefix + `:Header/>` + "\n")
	buf.WriteString(`  <` + gatewayEdiSoapPrefix + `:Body>` + "\n")
	buf.WriteString(`    <` + gw + gatewayEdiInquiryOperationElement + `>` + "\n")
	buf.WriteString(`      <` + gw + gatewayEdiInquiryContainerElement + `>` + "\n")
	buf.WriteString(`        <` + gw + gatewayEdiInquiryParametersElement + `>` + "\n")
	for _, pair := range pairs {
		buf.WriteString(`          <` + gw + gatewayEdiInquiryMyNameValueElement + `>` + "\n")
		buf.WriteString(`            <` + gw + gatewayEdiInquiryNameElement + `>`)
		gatewayEdiEscapeXML(&buf, pair.Name)
		buf.WriteString(`</` + gw + gatewayEdiInquiryNameElement + `>` + "\n")
		buf.WriteString(`            <` + gw + gatewayEdiInquiryValueElement + `>`)
		gatewayEdiEscapeXML(&buf, pair.Value)
		buf.WriteString(`</` + gw + gatewayEdiInquiryValueElement + `>` + "\n")
		buf.WriteString(`          </` + gw + gatewayEdiInquiryMyNameValueElement + `>` + "\n")
	}
	buf.WriteString(`        </` + gw + gatewayEdiInquiryParametersElement + `>` + "\n")
	buf.WriteString(`        <` + gw + gatewayEdiInquiryResponseDataTypeElement + `>` + gatewayEdiResponseDataTypeXml + `</` + gw + gatewayEdiInquiryResponseDataTypeElement + `>` + "\n")
	buf.WriteString(`      </` + gw + gatewayEdiInquiryContainerElement + `>` + "\n")
	buf.WriteString(`    </` + gw + gatewayEdiInquiryOperationElement + `>` + "\n")
	buf.WriteString(`  </` + gatewayEdiSoapPrefix + `:Body>` + "\n")
	buf.WriteString(`</` + gatewayEdiSoapPrefix + `:Envelope>` + "\n")

	return buf.Bytes(), nil
}

// gatewayEdiEscapeXML writes s with XML special characters escaped.
func gatewayEdiEscapeXML(buf *bytes.Buffer, s string) {
	_ = xml.EscapeText(buf, []byte(s))
}

// CheckEligibility runs the GatewayEDI eligibility check: it builds the SOAP
// 1.1 DoInquiry request from the caller's values, POSTs it to the configured
// gatewayEdiServiceUri with the configured GatewayEDI credentials as HTTP Basic
// auth, and interprets the SOAP response.
//
// No PGP encryption is applied to the request. The user's GatewayEDI keyring
// entry is still consulted so that an encrypted response can be decrypted with
// the private key, but it is no longer required: with ResponseDataType=Xml the
// gateway answers with plain SOAP XML, and the request itself no longer needs a
// public key.
func (g *GatewayEDIEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	// Build the SOAP request envelope: plain XML, no PGP.
	envelope, err := g.buildSoapEnvelope(values)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: build envelope: %w", err)
	}

	// Retrieve the plugin's options from user configuration.
	configs, err := model.GetConfigValues(userName)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: get config values: %w", err)
	}

	options := make(map[string]string, len(configs))
	for _, cfg := range configs {
		switch cfg.Option {
		case gatewayEdiServiceURIOption, gatewayEdiUsernameOption, gatewayEdiPasswordOption:
			options[cfg.Option] = cfg.Value
		}
	}

	serviceUri := options[gatewayEdiServiceURIOption]
	username := options[gatewayEdiUsernameOption]
	password := options[gatewayEdiPasswordOption]

	// Without an endpoint there is nothing to contact: fail loudly rather than
	// reporting an eligibility result that was never obtained.
	if strings.TrimSpace(serviceUri) == "" {
		return nil, fmt.Errorf("gatewayedi: %s is not configured for user '%s'",
			gatewayEdiServiceURIOption, userName)
	}

	// The vendor authenticates DoInquiry with HTTP Basic credentials, so a
	// missing username or password would only produce an unauthenticated call.
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("gatewayedi: %s and %s are not configured for user '%s'",
			gatewayEdiUsernameOption, gatewayEdiPasswordOption, userName)
	}

	// The response is PGP-decrypted only if the gateway encrypts it, so a
	// keyring entry is optional for this call.
	var privateKey []byte
	if key, keyErr := model.GetKeyringEntry(userName, GatewayEDIEligibilityKeyName); keyErr == nil {
		privateKey = key.PrivateKey
	}

	return g.postSoapRequest(serviceUri, username, password, envelope, privateKey, g.httpClient)
}

// postSoapRequest POSTs the SOAP request document to serviceUri with the
// GatewayEDI credentials as HTTP Basic auth, and interprets the SOAP response.
//
// The resolved service URI, credentials, payload and key material are passed
// in, and no database access happens here, so the complete HTTP + PGP-response
// handling path is unit-testable without a keyring or tUserConfig.
//
// Failures that occur before the request is sent (empty URI, unbuildable
// request, transport error) are returned as Go errors. Failures that occur
// after the request was sent (non-2xx status, undecryptable body, body that is
// not a recognisable SOAP eligibility result) are returned as a response with
// Status StatusServerError and SuccessCode SuccessCodeSystemError, so callers
// that only inspect the response still cannot mistake them for success.
func (g *GatewayEDIEligibility) postSoapRequest(serviceUri, username, password string, payload, privateKey []byte, client *http.Client) (*EligibilityResponse, error) {
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
	// Headers follow the SOAP 1.1 contract declared by the vendor WSDL: a
	// document/literal POST with the WSDL's SOAPAction, and the GatewayEDI
	// account credentials as HTTP Basic auth (what the Java 0.5.x plugin
	// configures on the Axis stub through Call.USERNAME_PROPERTY and
	// Call.PASSWORD_PROPERTY).
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", gatewayEdiSoapAction)
	req.SetBasicAuth(username, password)

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
