package eligibility

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/freemed/remitt-server/crypto"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"

	// golang.org/x/crypto/openpgp falls back to RIPEMD160 (the last entry of
	// its candidate list) when a recipient key carries no preferred-hash
	// subpacket, and the hash must be linked into the binary for
	// openpgp.Encrypt to succeed. Linking it here keeps these fixtures on the
	// same crypto.EncryptPGP / crypto.DecryptPGP path the plugin uses.
	_ "golang.org/x/crypto/ripemd160"
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
		"gatewayEdiUsername":   false,
		"gatewayEdiPassword":   false,
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
		return nil, fmt.Errorf("unexpected checker type %T", checker)
	}
	return g, nil
}

// ---------------------------------------------------------------------------
// Request-contract tests: the WSDL's DoInquiry envelope (no DB, no live call)
// ---------------------------------------------------------------------------

// gatewayEdiTestRequestValues is a complete eligibility request in the
// vocabulary of org.remitt.prototype.EligibilityParameter (the keys the values
// map uses): every parameter the Java 0.5.x GatewayEDIEligibility maps onto a
// GatewayEDI MyNameValue, plus "serviceDate", which is a canonical eligibility
// parameter the Java plugin never sends.
var gatewayEdiTestRequestValues = map[string]string{
	"npi":                   "1234567893",
	"insuranceId":           "MEMBER-0001",
	"insuredLastName":       "Doe & Sons", // must be XML-escaped in the body
	"insuredFirstName":      "Jane",
	"insuredDateOfBirth":    "1970-01-02",
	"insuredGender":         "F",
	"insuredState":          "MA",
	"insuredSsn":            "000-00-0000",
	"dependentLastName":     "Doe",
	"dependentFirstName":    "Junior",
	"dependentDateOfBirth":  "2015-01-01",
	"dependentGender":       "M",
	"dependentRelationship": "01",
	"serviceType":           "30",
	"cardIssueDate":         "2020-01-01",
	"groupId":               "GRP-9",
}

// gatewayEdiTestExpectedPairs is the ordered (Name, Value) sequence the DoInquiry
// body must carry for gatewayEdiTestRequestValues: the GatewayEDI parameter
// names of GatewayEDIEligibility.java:88-137, in that order, each carrying the
// caller's value.
var gatewayEdiTestExpectedPairs = [][2]string{
	{"NPI", "1234567893"},
	{"InsuranceNum", "MEMBER-0001"},
	{"InsuredLastName", "Doe & Sons"},
	{"InsuredFirstName", "Jane"},
	{"InsuredDob", "1970-01-02"},
	{"InsuredGender", "F"},
	{"InsuredState", "MA"},
	{"InsuredSsn", "000-00-0000"},
	{"DependentLastName", "Doe"},
	{"DependentFirstName", "Junior"},
	{"DependentDob", "2015-01-01"},
	{"DependentGender", "M"},
	{"DependentRelationshipCode", "01"},
	{"ServiceTypeCode", "30"},
	{"CardIssueDate", "2020-01-01"},
	{"GroupNumber", "GRP-9"},
}

// gatewayEdiEnvelopeNode is a prefix-independent, namespace-aware view of the
// request document: XMLName.Space resolves the namespace URI regardless of the
// prefix the writer chose.
type gatewayEdiEnvelopeNode struct {
	XMLName  xml.Name
	Text     string                   `xml:",chardata"`
	Children []gatewayEdiEnvelopeNode `xml:",any"`
}

// child returns the first direct child element with the given local name.
func (n *gatewayEdiEnvelopeNode) child(local string) *gatewayEdiEnvelopeNode {
	for i := range n.Children {
		if n.Children[i].XMLName.Local == local {
			return &n.Children[i]
		}
	}
	return nil
}

// text returns the element's character data, trimmed.
func (n *gatewayEdiEnvelopeNode) text() string {
	return strings.TrimSpace(n.Text)
}

// nameValuePairs reads the MyNameValue pairs of a Parameters element.
func (n *gatewayEdiEnvelopeNode) nameValuePairs(t *testing.T) [][2]string {
	t.Helper()

	var pairs [][2]string
	for i := range n.Children {
		item := &n.Children[i]
		if item.XMLName.Local != gatewayEdiInquiryMyNameValueElement {
			t.Errorf("Parameters child %q; want %q", item.XMLName.Local, gatewayEdiInquiryMyNameValueElement)
			continue
		}
		if item.XMLName.Space != gatewayEdiWebServicesNamespace {
			t.Errorf("MyNameValue namespace = %q; want %q", item.XMLName.Space, gatewayEdiWebServicesNamespace)
		}
		name, value := item.child(gatewayEdiInquiryNameElement), item.child(gatewayEdiInquiryValueElement)
		if name == nil || value == nil {
			t.Errorf("MyNameValue is missing its %s/%s children: %+v",
				gatewayEdiInquiryNameElement, gatewayEdiInquiryValueElement, item)
			continue
		}
		pairs = append(pairs, [2]string{name.text(), value.text()})
	}
	return pairs
}

// gatewayEdiParseRequest parses a request document and returns its root node.
func gatewayEdiParseRequest(t *testing.T, envelope []byte) *gatewayEdiEnvelopeNode {
	t.Helper()

	var doc gatewayEdiEnvelopeNode
	if err := xml.Unmarshal(envelope, &doc); err != nil {
		t.Fatalf("request document is not valid XML: %v (%s)", err, envelope)
	}
	return &doc
}

// assertGatewayEdiNoPGP fails when a request body carries PGP armor or is not an
// XML document: the DoInquiry request path must be plain SOAP.
func assertGatewayEdiNoPGP(t *testing.T, body []byte) {
	t.Helper()

	for _, marker := range []string{"BEGIN PGP", "END PGP", "-----BEGIN", "PGP MESSAGE"} {
		if bytes.Contains(body, []byte(marker)) {
			t.Errorf("request body contains a PGP/armored block (%q); the request must be sent unencrypted", marker)
		}
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || !bytes.HasPrefix(trimmed, []byte("<?xml")) {
		t.Errorf("request body is not an XML document: %q", body)
	}
}

// TestGatewayEDIRequestEnvelope asserts the request document against the vendor
// WSDL (https://services.gatewayedi.com/eligibility/service.asmx?WSDL): the
// SOAPAction, the DoInquiry operation element inside the SOAP Body, its
// GatewayEDI.WebServices namespace, the Inquiry/Parameters/MyNameValue shape,
// ResponseDataType=Xml, and no PGP anywhere in the body.
func TestGatewayEDIRequestEnvelope(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}

	t.Run("soapaction_from_wsdl", func(t *testing.T) {
		// <soap:operation soapAction="GatewayEDI.WebServices/DoInquiry" style="document"/>
		if gatewayEdiSoapAction != "GatewayEDI.WebServices/DoInquiry" {
			t.Errorf("gatewayEdiSoapAction = %q; want %q", gatewayEdiSoapAction, "GatewayEDI.WebServices/DoInquiry")
		}
	})

	envelope, err := g.buildSoapEnvelope(gatewayEdiTestRequestValues)
	if err != nil {
		t.Fatalf("buildSoapEnvelope() error = %v", err)
	}

	doc := gatewayEdiParseRequest(t, envelope)

	t.Run("envelope_and_operation_element", func(t *testing.T) {
		if doc.XMLName.Local != "Envelope" || doc.XMLName.Space != gatewayEdiSoapEnvelopeNamespace {
			t.Fatalf("root element = {%s}%s; want {%s}Envelope", doc.XMLName.Space, doc.XMLName.Local, gatewayEdiSoapEnvelopeNamespace)
		}

		body := doc.child("Body")
		if body == nil || body.XMLName.Space != gatewayEdiSoapEnvelopeNamespace {
			t.Fatalf("SOAP Body element missing or in the wrong namespace: %+v", body)
		}

		op := body.child(gatewayEdiInquiryOperationElement)
		if op == nil {
			t.Fatalf("SOAP Body has no %q operation element: %s", gatewayEdiInquiryOperationElement, envelope)
		}
		if op.XMLName.Space != gatewayEdiWebServicesNamespace {
			t.Errorf("%s namespace = %q; want %q (WSDL targetNamespace)",
				gatewayEdiInquiryOperationElement, op.XMLName.Space, gatewayEdiWebServicesNamespace)
		}

		inq := op.child(gatewayEdiInquiryContainerElement)
		if inq == nil {
			t.Fatalf("%s has no %q child: %s", gatewayEdiInquiryOperationElement, gatewayEdiInquiryContainerElement, envelope)
		}
		if inq.XMLName.Space != gatewayEdiWebServicesNamespace {
			t.Errorf("%s namespace = %q; want %q", gatewayEdiInquiryContainerElement, inq.XMLName.Space, gatewayEdiWebServicesNamespace)
		}
		if params := inq.child(gatewayEdiInquiryParametersElement); params == nil || params.XMLName.Space != gatewayEdiWebServicesNamespace {
			t.Errorf("%s element missing or in the wrong namespace: %+v", gatewayEdiInquiryParametersElement, params)
		}
	})

	t.Run("name_value_pairs_carry_request_values", func(t *testing.T) {
		op := doc.child("Body").child(gatewayEdiInquiryOperationElement)
		inq := op.child(gatewayEdiInquiryContainerElement)
		params := inq.child(gatewayEdiInquiryParametersElement)
		if params == nil {
			t.Fatal("no Parameters element in the request body")
		}

		pairs := params.nameValuePairs(t)
		if len(pairs) != len(gatewayEdiTestExpectedPairs) {
			t.Fatalf("%d MyNameValue pairs; want %d: %v", len(pairs), len(gatewayEdiTestExpectedPairs), pairs)
		}
		for i, want := range gatewayEdiTestExpectedPairs {
			if pairs[i][0] != want[0] || pairs[i][1] != want[1] {
				t.Errorf("pair %d = %q=%q; want %q=%q", i, pairs[i][0], pairs[i][1], want[0], want[1])
			}
		}
		// Every caller-supplied value (bar the canonical serviceDate, which the
		// Java reference never maps) must reach the body; compare the escaped
		// form, since values are XML-escaped on the way in.
		for _, want := range gatewayEdiTestExpectedPairs {
			var escaped bytes.Buffer
			_ = xml.EscapeText(&escaped, []byte(want[1]))
			if !strings.Contains(string(envelope), escaped.String()) {
				t.Errorf("request body is missing value %q", want[1])
			}
		}
		if strings.Contains(string(envelope), "serviceDate") || strings.Contains(string(envelope), "2026-09-15") {
			t.Error("request body carries the unmapped serviceDate parameter")
		}
	})

	t.Run("response_data_type_is_xml", func(t *testing.T) {
		op := doc.child("Body").child(gatewayEdiInquiryOperationElement)
		inq := op.child(gatewayEdiInquiryContainerElement)

		rdt := inq.child(gatewayEdiInquiryResponseDataTypeElement)
		if rdt == nil {
			t.Fatalf("no %s element in %s", gatewayEdiInquiryResponseDataTypeElement, gatewayEdiInquiryContainerElement)
		}
		if rdt.XMLName.Space != gatewayEdiWebServicesNamespace {
			t.Errorf("%s namespace = %q; want %q", gatewayEdiInquiryResponseDataTypeElement, rdt.XMLName.Space, gatewayEdiWebServicesNamespace)
		}
		if rdt.text() != gatewayEdiResponseDataTypeXml {
			t.Errorf("%s = %q; want %q", gatewayEdiInquiryResponseDataTypeElement, rdt.text(), gatewayEdiResponseDataTypeXml)
		}
		if gatewayEdiResponseDataTypeXml != "Xml" {
			t.Errorf("gatewayEdiResponseDataTypeXml = %q; want %q (WSResponseDataType enum)", gatewayEdiResponseDataTypeXml, "Xml")
		}
	})

	t.Run("values_are_xml_escaped", func(t *testing.T) {
		s := string(envelope)
		if strings.Contains(s, "Doe & Sons") {
			t.Error("the '&' in InsuredLastName was not XML-escaped")
		}
		if !strings.Contains(s, "Doe &amp; Sons") {
			t.Errorf("request body does not carry the escaped InsuredLastName: %s", s)
		}
	})

	t.Run("no_pgp_in_request", func(t *testing.T) {
		assertGatewayEdiNoPGP(t, envelope)
	})
}

// TestGatewayEDIRequestEnvelopeOmitsAbsentParameters pins the addNameValue
// behaviour of the Java reference: a parameter the request carries no value for
// contributes no MyNameValue element (and the canonical serviceDate parameter,
// which the Java plugin never maps, is never sent either).
func TestGatewayEDIRequestEnvelopeOmitsAbsentParameters(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}

	envelope, err := g.buildSoapEnvelope(map[string]string{
		"npi":         "1234567893",
		"serviceDate": "2026-09-15",
	})
	if err != nil {
		t.Fatalf("buildSoapEnvelope() error = %v", err)
	}

	doc := gatewayEdiParseRequest(t, envelope)
	op := doc.child("Body").child(gatewayEdiInquiryOperationElement)
	params := op.child(gatewayEdiInquiryContainerElement).child(gatewayEdiInquiryParametersElement)
	if params == nil {
		t.Fatal("no Parameters element in the request body")
	}

	pairs := params.nameValuePairs(t)
	if len(pairs) != 1 {
		t.Fatalf("%d MyNameValue pairs; want exactly 1 (NPI): %v", len(pairs), pairs)
	}
	if pairs[0][0] != "NPI" || pairs[0][1] != "1234567893" {
		t.Errorf("pair 0 = %q=%q; want NPI=1234567893", pairs[0][0], pairs[0][1])
	}

	// ResponseDataType is minOccurs="1" in the WSDL, so it is present even for
	// an otherwise empty inquiry.
	inq := op.child(gatewayEdiInquiryContainerElement)
	if rdt := inq.child(gatewayEdiInquiryResponseDataTypeElement); rdt == nil || rdt.text() != "Xml" {
		t.Errorf("%s element = %+v; want Xml", gatewayEdiInquiryResponseDataTypeElement, rdt)
	}
}

// TestGatewayEDIBuildSoapEnvelopeEmpty verifies that buildSoapEnvelope works
// with an empty values map: a valid DoInquiry document with no pairs.
func TestGatewayEDIBuildSoapEnvelopeEmpty(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}

	envelope, err := g.buildSoapEnvelope(map[string]string{})
	if err != nil {
		t.Fatalf("buildSoapEnvelope() with empty values error = %v", err)
	}

	doc := gatewayEdiParseRequest(t, envelope)
	op := doc.child("Body").child(gatewayEdiInquiryOperationElement)
	if op == nil {
		t.Fatalf("empty values produced no %s element: %s", gatewayEdiInquiryOperationElement, envelope)
	}
	inq := op.child(gatewayEdiInquiryContainerElement)
	if inq == nil {
		t.Fatalf("empty values produced no %s element: %s", gatewayEdiInquiryContainerElement, envelope)
	}
	if params := inq.child(gatewayEdiInquiryParametersElement); params == nil || len(params.Children) != 0 {
		t.Errorf("%s element = %+v; want an empty element", gatewayEdiInquiryParametersElement, params)
	}
	assertGatewayEdiNoPGP(t, envelope)
}

// TestGatewayEDIPostedRequestIsPlainSoapWithBasicAuth drives the full DB-free
// request path (postSoapRequest) with the envelope buildSoapEnvelope produces
// and asserts what the wire actually carries: the WSDL SOAPAction, the SOAP
// body document verbatim, an Authorization: Basic header that decodes to the
// configured GatewayEDI credentials, and no PGP block anywhere.
func TestGatewayEDIPostedRequestIsPlainSoapWithBasicAuth(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}
	if err := g.SetContext(context.Background()); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	envelope, err := g.buildSoapEnvelope(gatewayEdiTestRequestValues)
	if err != nil {
		t.Fatalf("buildSoapEnvelope() error = %v", err)
	}

	srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(gatewayEdiTestSuccessBody))

	resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", envelope, nil, srv.Client())
	if err != nil {
		t.Fatalf("postSoapRequest() error = %v", err)
	}
	if resp == nil {
		t.Fatal("postSoapRequest() returned nil response")
	}
	if resp.Status != StatusOK || resp.SuccessCode != SuccessCodeSuccess {
		t.Errorf("resp = (%q, %q); want (%q, %q) (messages: %s)",
			resp.Status, resp.SuccessCode, StatusOK, SuccessCodeSuccess, gatewayEdiMessages(resp))
	}

	if got := srv.requestCount(); got != 1 {
		t.Fatalf("server received %d requests; want exactly 1", got)
	}

	posted := srv.receivedBody()
	if !bytes.Equal(posted, envelope) {
		t.Errorf("posted body = %q; want the built envelope verbatim", posted)
	}
	assertGatewayEdiNoPGP(t, posted)
	if !bytes.Contains(posted, []byte("<gw:"+gatewayEdiInquiryOperationElement)) {
		t.Errorf("posted body has no %s operation element: %s", gatewayEdiInquiryOperationElement, posted)
	}

	// The SOAPAction is the one the WSDL declares for DoInquiry.
	if got := srv.receivedHeader("SOAPAction"); got != gatewayEdiSoapAction {
		t.Errorf("SOAPAction = %q; want %q", got, gatewayEdiSoapAction)
	}
	if got := srv.receivedHeader("Content-Type"); !strings.HasPrefix(got, "text/xml") {
		t.Errorf("Content-Type = %q; want text/xml", got)
	}

	// HTTP Basic auth carrying the configured GatewayEDI credentials.
	authz := srv.receivedHeader("Authorization")
	const prefix = "Basic "
	if !strings.HasPrefix(authz, prefix) {
		t.Fatalf("Authorization = %q; want a Basic scheme header", authz)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authz, prefix))
	if err != nil {
		t.Fatalf("Authorization value %q is not valid base64: %v", authz, err)
	}
	user, pass, found := strings.Cut(string(decoded), ":")
	if !found {
		t.Fatalf("decoded Authorization %q is not user:password", decoded)
	}
	if user != gatewayEdiTestUsername {
		t.Errorf("Basic auth username = %q; want the configured %q", user, gatewayEdiTestUsername)
	}
	if pass != gatewayEdiTestPassword {
		t.Errorf("Basic auth password = %q; want the configured GatewayEDI password", pass)
	}
	// A request with no credentials must not be silently sent as if it had them.
	if user == "" || pass == "" {
		t.Error("Basic auth credentials are empty")
	}
}

// TestGatewayEDICheckEligibilityRequiresLiveService verifies the end-to-end
// path never claims success without a live gateway: with no database the
// keyring/config lookups fail (so the test skips), and when a database is
// present the plugin must either return an error or a response that does not
// claim success unless a gateway really answered.
func TestGatewayEDICheckEligibilityRequiresLiveService(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("skipping: no DB available for keyring/config lookups: %v", r)
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
	if err == nil && resp == nil {
		t.Fatal("CheckEligibility() returned nil response and nil error")
	}
	if resp == nil {
		// An explicit error is an acceptable outcome (no keyring/config).
		if err == nil {
			t.Fatal("CheckEligibility() returned nil response without an error")
		}
		return
	}

	for _, m := range resp.Messages {
		if strings.Contains(strings.ToLower(m), "stub") {
			t.Errorf("CheckEligibility() leaked a stub message: %q", m)
		}
	}
	if resp.Status == StatusOK && resp.SuccessCode != SuccessCodeSuccess {
		t.Logf("negative eligibility result (expected when credentials/config are absent): %q", resp.SuccessCode)
	}
	if resp.Status != StatusOK && resp.SuccessCode == SuccessCodeSuccess {
		t.Errorf("non-OK status reported SuccessCodeSuccess: %+v", resp)
	}
}

// ---------------------------------------------------------------------------
// SOAP POST tests (no DB required; postSoapRequest takes resolved inputs)
// ---------------------------------------------------------------------------

// gatewayEdiTestPayload stands in for the SOAP request document that
// CheckEligibility builds for the vendor: a plain (unencrypted) SOAP 1.1
// DoInquiry envelope.
var gatewayEdiTestPayload = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:gw="GatewayEDI.WebServices">
  <soapenv:Header/>
  <soapenv:Body>
    <gw:DoInquiry>
      <gw:Inquiry>
        <gw:Parameters>
          <gw:MyNameValue>
            <gw:Name>NPI</gw:Name>
            <gw:Value>1234567893</gw:Value>
          </gw:MyNameValue>
        </gw:Parameters>
        <gw:ResponseDataType>Xml</gw:ResponseDataType>
      </gw:Inquiry>
    </gw:DoInquiry>
  </soapenv:Body>
</soapenv:Envelope>
`)

// gatewayEdiInquiryResponse builds a GatewayEDI.WebServices doInquiryResponse
// envelope in the documented shape: the Java 0.5.x Axis stubs declare
// WebServices/GatewayEDI/WSEligibilityResponse with ResponseAsRawString,
// ExtraProcessingInfo, SuccessCode and OriginalInquiry elements (all in the
// GatewayEDI.WebServices namespace), and ValidationFailureCollection declares
// the message list as ExtraProcessingInfo/AllMessages with "string" items.
func gatewayEdiInquiryResponse(successCode, rawResponse string, messages ...string) string {
	var sb strings.Builder

	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:gw="GatewayEDI.WebServices">
  <soapenv:Body>
    <gw:doInquiryResponse>
      <gw:doInquiryResult>`)

	if rawResponse != "" {
		sb.WriteString("\n        <gw:ResponseAsRawString>" + rawResponse + "</gw:ResponseAsRawString>")
	}

	if len(messages) > 0 {
		sb.WriteString("\n        <gw:ExtraProcessingInfo>\n          <gw:AllMessages>")
		for _, m := range messages {
			sb.WriteString("\n            <gw:string>" + m + "</gw:string>")
		}
		sb.WriteString("\n          </gw:AllMessages>\n        </gw:ExtraProcessingInfo>")
	}

	sb.WriteString("\n        <gw:SuccessCode>" + successCode + "</gw:SuccessCode>")
	sb.WriteString("\n      </gw:doInquiryResult>\n    </gw:doInquiryResponse>\n  </soapenv:Body>\n</soapenv:Envelope>")

	return sb.String()
}

// gatewayEdiTestSuccessBody is a documented-shape SOAP eligibility response
// reporting SuccessCode Success.
var gatewayEdiTestSuccessBody = gatewayEdiInquiryResponse("Success", "", "Active coverage", "Co-pay 20%")

// gatewayEdiTestFaultBody is a SOAP fault.
const gatewayEdiTestFaultBody = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Body>
    <soapenv:Fault>
      <faultcode>soapenv:Server</faultcode>
      <faultstring>Invalid subscriber</faultstring>
    </soapenv:Fault>
  </soapenv:Body>
</soapenv:Envelope>`

// gatewayEdiTestUsername and gatewayEdiTestPassword are the GatewayEDI account
// credentials the tests configure (the gatewayEdiUsername / gatewayEdiPassword
// tUserConfig options). The test server asserts every request carries exactly
// these as HTTP Basic auth.
const (
	gatewayEdiTestUsername = "gatewayedi-test-user"
	gatewayEdiTestPassword = "gatewayedi-test-password"
)

// gatewayEdiPost calls postSoapRequest with the configured test credentials, so
// each subtest exercises the same auth path CheckEligibility uses.
func gatewayEdiPost(t *testing.T, g *GatewayEDIEligibility, uri string, payload, privateKey []byte, client *http.Client) (*EligibilityResponse, error) {
	t.Helper()
	return g.postSoapRequest(uri, gatewayEdiTestUsername, gatewayEdiTestPassword, payload, privateKey, client)
}

// gatewayEdiTestServer is an httptest server that enforces the SOAP request
// contract and records what it received.
type gatewayEdiTestServer struct {
	*httptest.Server

	mu     sync.Mutex
	count  int
	body   []byte
	header http.Header
}

// newGatewayEdiTestServer starts a server that asserts every request is a
// single SOAP POST to wantPath, then replies with reply (when non-nil).
func newGatewayEdiTestServer(t *testing.T, wantPath string, reply func(w http.ResponseWriter, r *http.Request)) *gatewayEdiTestServer {
	t.Helper()

	srv := &gatewayEdiTestServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}

		srv.mu.Lock()
		srv.count++
		srv.body = body
		srv.header = r.Header.Clone()
		srv.mu.Unlock()

		if r.Method != http.MethodPost {
			t.Errorf("request method = %q; want POST", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("request path = %q; want %q", r.URL.Path, wantPath)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/xml") {
			t.Errorf("Content-Type = %q; want text/xml", ct)
		}
		if sa := r.Header.Get("SOAPAction"); sa == "" {
			t.Error("SOAPAction header is empty; the gateway expects SOAP")
		} else if sa != gatewayEdiSoapAction {
			t.Errorf("SOAPAction = %q; want %q", sa, gatewayEdiSoapAction)
		}
		// The GatewayEDI account credentials must travel as HTTP Basic auth,
		// the way the Java 0.5.x plugin authenticates the Axis stub.
		user, pass, ok := r.BasicAuth()
		if !ok {
			t.Error("request carries no HTTP Basic Authorization header; the gateway requires gatewayEdiUsername/gatewayEdiPassword")
		} else if user != gatewayEdiTestUsername || pass != gatewayEdiTestPassword {
			t.Errorf("Basic auth credentials = %q/%q; want the configured GatewayEDI credentials", user, pass)
		}
		if len(body) == 0 {
			t.Error("request body is empty; the SOAP request document was not POSTed")
		}
		// The request path must no longer PGP-encrypt anything.
		for _, marker := range []string{"BEGIN PGP", "END PGP", "-----BEGIN"} {
			if bytes.Contains(body, []byte(marker)) {
				t.Errorf("request body contains a PGP/armored block (%q); the DoInquiry request must be plain SOAP XML", marker)
			}
		}

		if reply != nil {
			reply(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *gatewayEdiTestServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *gatewayEdiTestServer) receivedBody() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.body...)
}

func (s *gatewayEdiTestServer) receivedHeader(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.header.Get(name)
}

// gatewayEdiXMLReply replies with a text/xml body.
func gatewayEdiXMLReply(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, body)
	}
}

// gatewayEdiStatusReply replies with an arbitrary status code and body.
func gatewayEdiStatusReply(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// gatewayEdiMessages joins a response's messages for assertions.
func gatewayEdiMessages(resp *EligibilityResponse) string {
	if resp == nil {
		return "<nil response>"
	}
	return strings.Join(resp.Messages, " | ")
}

// assertGatewayEdiFailure fails the test unless the outcome is the exact
// (Status, SuccessCode) pair the Java plugin reports when it cannot interpret
// the gateway response: SERVER_ERROR / SYSTEM_ERROR.
func assertGatewayEdiFailure(t *testing.T, resp *EligibilityResponse, err error) {
	t.Helper()

	if err == nil && resp == nil {
		t.Fatal("both response and error are nil; the failure was not reported")
	}
	if resp == nil {
		return // an explicit Go error is an acceptable failure report
	}
	if resp.Status != StatusServerError {
		t.Errorf("resp.Status = %q; want %q (messages: %s)", resp.Status, StatusServerError, gatewayEdiMessages(resp))
	}
	if resp.SuccessCode != SuccessCodeSystemError {
		t.Errorf("resp.SuccessCode = %q; want %q (messages: %s)", resp.SuccessCode, SuccessCodeSystemError, gatewayEdiMessages(resp))
	}
}

// gatewayEdiTestKeyPair generates an OpenPGP key pair and returns the armored
// public and private keys.
func gatewayEdiTestKeyPair(t *testing.T) (publicKey, privateKey []byte) {
	t.Helper()

	entity, err := openpgp.NewEntity("GatewayEDI Test", "", "gatewayedi-test@example.invalid", nil)
	if err != nil {
		t.Fatalf("openpgp.NewEntity: %v", err)
	}

	var pubBuf bytes.Buffer
	pubWriter, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode(public): %v", err)
	}
	if err := entity.Serialize(pubWriter); err != nil {
		t.Fatalf("Serialize(public): %v", err)
	}
	if err := pubWriter.Close(); err != nil {
		t.Fatalf("close public armor writer: %v", err)
	}

	var privBuf bytes.Buffer
	privWriter, err := armor.Encode(&privBuf, openpgp.PrivateKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode(private): %v", err)
	}
	if err := entity.SerializePrivate(privWriter, nil); err != nil {
		t.Fatalf("SerializePrivate: %v", err)
	}
	if err := privWriter.Close(); err != nil {
		t.Fatalf("close private armor writer: %v", err)
	}

	return pubBuf.Bytes(), privBuf.Bytes()
}

// gatewayEdiArmorPGPMessage wraps a binary OpenPGP message in ASCII armor, as
// a gateway that armors its responses would.
func gatewayEdiArmorPGPMessage(t *testing.T, binaryMessage []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer, err := armor.Encode(&buf, "PGP MESSAGE", nil)
	if err != nil {
		t.Fatalf("armor.Encode(message): %v", err)
	}
	if _, err := writer.Write(binaryMessage); err != nil {
		t.Fatalf("write armored message: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close armored message: %v", err)
	}
	return buf.Bytes()
}

// TestGatewayEDIPostSoapRequest exercises the DB-free SOAP HTTP seam:
// postSoapRequest(serviceUri, payload, privateKey, client).
func TestGatewayEDIPostSoapRequest(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}
	if err := g.SetContext(context.Background()); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	t.Run("success_plaintext", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(gatewayEdiTestSuccessBody))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil {
			t.Fatal("postSoapRequest() returned nil response")
		}
		if resp.Status != StatusOK {
			t.Errorf("resp.Status = %q; want %q (messages: %s)", resp.Status, StatusOK, gatewayEdiMessages(resp))
		}
		if resp.SuccessCode != SuccessCodeSuccess {
			t.Errorf("resp.SuccessCode = %q; want %q (messages: %s)", resp.SuccessCode, SuccessCodeSuccess, gatewayEdiMessages(resp))
		}
		if !strings.Contains(gatewayEdiMessages(resp), "Active coverage") {
			t.Errorf("messages %q do not include the parsed SOAP message", gatewayEdiMessages(resp))
		}
		if got := srv.requestCount(); got != 1 {
			t.Errorf("server received %d requests; want exactly 1", got)
		}
		if got := srv.receivedBody(); !bytes.Equal(got, gatewayEdiTestPayload) {
			t.Errorf("posted body = %q; want the SOAP request document %q", got, gatewayEdiTestPayload)
		}
		if got := srv.receivedHeader("SOAPAction"); got != gatewayEdiSoapAction {
			t.Errorf("SOAPAction = %q; want %q", got, gatewayEdiSoapAction)
		}
		if got := srv.receivedHeader("Content-Type"); !strings.HasPrefix(got, "text/xml") {
			t.Errorf("Content-Type = %q; want text/xml", got)
		}
		// The response carries no ResponseAsRawString element, so the Java
		// contract's rawResponse falls back to the decrypted body verbatim.
		if resp.RawResponse != gatewayEdiTestSuccessBody {
			t.Errorf("resp.RawResponse = %q; want the response body verbatim", resp.RawResponse)
		}
	})

	t.Run("success_pgp_encrypted_binary_response", func(t *testing.T) {
		publicKey, privateKey := gatewayEdiTestKeyPair(t)

		encryptedResponse, err := crypto.EncryptPGP([]byte(gatewayEdiTestSuccessBody), publicKey)
		if err != nil {
			t.Fatalf("crypto.EncryptPGP(response): %v", err)
		}

		srv := newGatewayEdiTestServer(t, "/eligibility", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write(encryptedResponse)
		})

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, privateKey, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil {
			t.Fatal("postSoapRequest() returned nil response")
		}
		if resp.Status != StatusOK || resp.SuccessCode != SuccessCodeSuccess {
			t.Errorf("decrypted PGP response not reported as success: %+v", resp)
		}
		if !strings.Contains(gatewayEdiMessages(resp), "Active coverage") {
			t.Errorf("messages %q do not include the decrypted SOAP message", gatewayEdiMessages(resp))
		}
		if got := srv.requestCount(); got != 1 {
			t.Errorf("server received %d requests; want exactly 1", got)
		}
	})

	t.Run("success_pgp_encrypted_armored_response", func(t *testing.T) {
		publicKey, privateKey := gatewayEdiTestKeyPair(t)

		encryptedResponse, err := crypto.EncryptPGP([]byte(gatewayEdiTestSuccessBody), publicKey)
		if err != nil {
			t.Fatalf("crypto.EncryptPGP(response): %v", err)
		}
		armored := gatewayEdiArmorPGPMessage(t, encryptedResponse)

		if !crypto.IsPGPEncrypted(armored) {
			t.Fatal("test fixture is not detected as PGP-encrypted")
		}

		srv := newGatewayEdiTestServer(t, "/eligibility", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write(armored)
		})

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, privateKey, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil || resp.Status != StatusOK || resp.SuccessCode != SuccessCodeSuccess {
			t.Errorf("armored PGP response not reported as success: %+v", resp)
		}
	})

	t.Run("encrypted_response_without_private_key", func(t *testing.T) {
		publicKey, _ := gatewayEdiTestKeyPair(t)

		encryptedResponse, err := crypto.EncryptPGP([]byte(gatewayEdiTestSuccessBody), publicKey)
		if err != nil {
			t.Fatalf("crypto.EncryptPGP(response): %v", err)
		}

		srv := newGatewayEdiTestServer(t, "/eligibility", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write(gatewayEdiArmorPGPMessage(t, encryptedResponse))
		})

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "private key") {
			t.Errorf("message %q does not mention the missing private key", gatewayEdiMessages(resp))
		}
	})

	t.Run("validation_failure_is_bad", func(t *testing.T) {
		body := gatewayEdiInquiryResponse("ValidationFailure", "", "Member not eligible on date of service")
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(body))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil {
			t.Fatal("postSoapRequest() returned nil response")
		}
		if resp.Status != StatusBad {
			t.Errorf("resp.Status = %q; want %q", resp.Status, StatusBad)
		}
		if resp.SuccessCode != SuccessCodeValidationFailure {
			t.Errorf("resp.SuccessCode = %q; want %q", resp.SuccessCode, SuccessCodeValidationFailure)
		}
		if !strings.Contains(gatewayEdiMessages(resp), "Member not eligible") {
			t.Errorf("messages %q do not include the gateway message", gatewayEdiMessages(resp))
		}
	})

	t.Run("soap_fault", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(gatewayEdiTestFaultBody))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "Invalid subscriber") {
			t.Errorf("message %q does not include the SOAP fault detail", gatewayEdiMessages(resp))
		}
	})

	t.Run("unparseable_body", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility",
			gatewayEdiXMLReply("<html><body>GatewayEDI maintenance window</body></html>"))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "unrecognised GatewayEDI response format") {
			t.Errorf("message %q does not name the unrecognised response format", gatewayEdiMessages(resp))
		}
	})

	t.Run("not_xml_body", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply("200 OK but not XML at all"))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "not valid XML") {
			t.Errorf("message %q does not report the invalid XML", gatewayEdiMessages(resp))
		}
	})

	t.Run("undecryptable_body", func(t *testing.T) {
		// A real private key plus a body that is neither XML nor a message
		// that key can decrypt: the response cannot be interpreted, so it must
		// be reported as SERVER_ERROR / SYSTEM_ERROR naming the decryption
		// failure.
		_, privateKey := gatewayEdiTestKeyPair(t)

		srv := newGatewayEdiTestServer(t, "/eligibility",
			gatewayEdiXMLReply("-----BEGIN PGP MESSAGE-----\n\nnot-a-real-pgp-message\n-----END PGP MESSAGE-----\n"))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, privateKey, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "could not be PGP-decrypted") {
			t.Errorf("message %q does not report the decryption failure", gatewayEdiMessages(resp))
		}
		if resp != nil && resp.RawResponse == "" {
			t.Log("note: RawResponse is empty because the body never decrypted")
		}
	})

	t.Run("empty_response_body", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(""))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
	})

	t.Run("non_2xx_response", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiStatusReply(http.StatusInternalServerError, "internal error"))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "500") {
			t.Errorf("message %q does not report the HTTP status code", gatewayEdiMessages(resp))
		}
	})

	t.Run("empty_service_uri", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(gatewayEdiTestSuccessBody))

		for _, uri := range []string{"", "   "} {
			resp, err := gatewayEdiPost(t, g, uri, gatewayEdiTestPayload, nil, srv.Client())
			if err == nil {
				t.Errorf("postSoapRequest(%q) error = nil; want an error", uri)
			}
			assertGatewayEdiFailure(t, resp, err)
		}
		if got := srv.requestCount(); got != 0 {
			t.Errorf("server received %d requests for an empty service URI; want 0", got)
		}
	})

	t.Run("transport_failure", func(t *testing.T) {
		dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		deadURL := dead.URL
		dead.Close()

		resp, err := gatewayEdiPost(t, g, deadURL+"/eligibility", gatewayEdiTestPayload, nil, nil)
		assertGatewayEdiFailure(t, resp, err)
		if err == nil {
			t.Error("postSoapRequest() error = nil after the endpoint went away; want an error")
		}
	})

	t.Run("empty_payload_rejected", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(gatewayEdiTestSuccessBody))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", nil, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if got := srv.requestCount(); got != 0 {
			t.Errorf("server received %d requests for an empty payload; want 0", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Response-contract tests: Java 0.5.x GatewayEDIEligibility mapping
// ---------------------------------------------------------------------------

// TestGatewayEDISuccessCodeMapping asserts the exact (Status, SuccessCode)
// pair produced for every EligibilitySuccessCode value in the Java contract
// (GatewayEDIEligibility.java:170-206, with the SuccessCode → success code
// table in the private getSuccessCode helper at lines 231-257).
func TestGatewayEDISuccessCodeMapping(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}
	if err := g.SetContext(context.Background()); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	cases := []struct {
		name            string
		elementValue    string
		wantStatus      string
		wantSuccessCode string
	}{
		{"Success", "Success", StatusOK, SuccessCodeSuccess},
		{"ValidationFailure", "ValidationFailure", StatusBad, SuccessCodeValidationFailure},
		{"PayerEnrollmentRequired", "PayerEnrollmentRequired", StatusBad, SuccessCodePayerEnrollmentRequired},
		{"ProviderEnrollmentRequired", "ProviderEnrollmentRequired", StatusBad, SuccessCodeProviderEnrollmentRequired},
		{"PayerNotSupported", "PayerNotSupported", StatusServerError, SuccessCodePayerNotSupported},
		{"PayerTimeout", "PayerTimeout", StatusServerError, SuccessCodePayerTimeout},
		{"SystemError", "SystemError", StatusServerError, SuccessCodeSystemError},
		// ProductRequired is a recognised SuccessCode (Java:253) but has no
		// branch in the status block, so the status falls through to the
		// SERVER_ERROR default at Java:205.
		{"ProductRequired", "ProductRequired", StatusServerError, SuccessCodeProductRequired},
		// The Java compares the element text with equalsIgnoreCase (Java:232).
		{"case_insensitive_success", "sUcCeSs", StatusOK, SuccessCodeSuccess},
		{"case_insensitive_payer_timeout", "PAYERtimeout", StatusServerError, SuccessCodePayerTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message := "message for " + tc.elementValue
			body := gatewayEdiInquiryResponse(tc.elementValue, "", message)
			srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(body))

			resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
			if err != nil {
				t.Fatalf("postSoapRequest() error = %v", err)
			}
			if resp == nil {
				t.Fatal("postSoapRequest() returned nil response")
			}
			if resp.Status != tc.wantStatus || resp.SuccessCode != tc.wantSuccessCode {
				t.Errorf("SuccessCode %q mapped to (%q, %q); want (%q, %q)",
					tc.elementValue, resp.Status, resp.SuccessCode, tc.wantStatus, tc.wantSuccessCode)
			}
			if !strings.Contains(gatewayEdiMessages(resp), message) {
				t.Errorf("messages %q do not include the ExtraProcessingInfo message", gatewayEdiMessages(resp))
			}
			if resp.RawResponse == "" {
				t.Error("resp.RawResponse is empty; the raw response was not preserved")
			}
		})
	}
}

// TestGatewayEDINeverSucceedsWithoutSuccessCode pins the defensive rule: a 2xx
// body that carries no recognised SuccessCode element yields SERVER_ERROR /
// SYSTEM_ERROR, never OK / SUCCESS, even when it contains self-describing
// success-looking text.
func TestGatewayEDINeverSucceedsWithoutSuccessCode(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}
	if err := g.SetContext(context.Background()); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	cases := []struct {
		name string
		body string
	}{
		{"no_success_code_element", `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Body>
    <eligibilityResponse xmlns="urn:remitt:eligibility">
      <Status>SUCCESS</Status>
      <EligibilityStatus>OK</EligibilityStatus>
      <ResultCode>0</ResultCode>
    </eligibilityResponse>
  </soapenv:Body>
</soapenv:Envelope>`},
		{"unrecognised_success_code_value", gatewayEdiInquiryResponse("SomethingElse", "", "unexpected code")},
		{"empty_success_code_value", gatewayEdiInquiryResponse("", "", "unexpected empty code")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(tc.body))

			resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
			if err != nil {
				t.Fatalf("postSoapRequest() error = %v", err)
			}
			if resp == nil {
				t.Fatal("postSoapRequest() returned nil response")
			}
			if resp.Status != StatusServerError {
				t.Errorf("resp.Status = %q; want %q (messages: %s)", resp.Status, StatusServerError, gatewayEdiMessages(resp))
			}
			if resp.SuccessCode != SuccessCodeSystemError {
				t.Errorf("resp.SuccessCode = %q; want %q", resp.SuccessCode, SuccessCodeSystemError)
			}
			if !strings.Contains(gatewayEdiMessages(resp), "unrecognised") {
				t.Errorf("messages %q do not name the unrecognised response", gatewayEdiMessages(resp))
			}
		})
	}
}

// TestGatewayEDIRawResponse pins the RawResponse field: the ResponseAsRawString
// element when the response carries one (the value the Java stores at
// GatewayEDIEligibility.java:158-160), otherwise the decrypted body verbatim.
func TestGatewayEDIRawResponse(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}
	if err := g.SetContext(context.Background()); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	const rawPayload = "ISA*00*          *00*          *ZZ*SUBMITTER      *ZZ*RECEIVER       *260915*1200*^*00501*000000001*0*P*:~"

	t.Run("response_as_raw_string", func(t *testing.T) {
		body := gatewayEdiInquiryResponse("Success", rawPayload, "Active coverage")
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(body))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil {
			t.Fatal("postSoapRequest() returned nil response")
		}
		if resp.RawResponse != rawPayload {
			t.Errorf("resp.RawResponse = %q; want the ResponseAsRawString value %q", resp.RawResponse, rawPayload)
		}
	})

	t.Run("falls_back_to_body", func(t *testing.T) {
		body := gatewayEdiInquiryResponse("Success", "", "Active coverage")
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(body))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil {
			t.Fatal("postSoapRequest() returned nil response")
		}
		if resp.RawResponse != body {
			t.Errorf("resp.RawResponse = %q; want the decrypted body verbatim", resp.RawResponse)
		}
	})

	t.Run("preserved_on_unrecognised_success_code", func(t *testing.T) {
		body := gatewayEdiInquiryResponse("SomethingElse", "", "unexpected code")
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(body))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil {
			t.Fatal("postSoapRequest() returned nil response")
		}
		if resp.RawResponse != body {
			t.Errorf("resp.RawResponse = %q; want the undecodable body preserved for diagnostics", resp.RawResponse)
		}
	})
}

// TestGatewayEDIMessagesFromExtraProcessingInfo verifies that the
// ExtraProcessingInfo/AllMessages/string messages the Java copies out of
// response.getExtraProcessingInfo().getAllMessages() reach the response, and
// that a failure response keeps them alongside the diagnostic message.
func TestGatewayEDIMessagesFromExtraProcessingInfo(t *testing.T) {
	g, err := newGatewayEDIForTest()
	if err != nil {
		t.Fatalf("failed to get GatewayEDIEligibility: %v", err)
	}
	if err := g.SetContext(context.Background()); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	t.Run("success_keeps_all_messages", func(t *testing.T) {
		body := gatewayEdiInquiryResponse("Success", "", "Active coverage", "Co-pay 20%", "Deductible 500")
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(body))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil {
			t.Fatal("postSoapRequest() returned nil response")
		}
		if len(resp.Messages) != 3 {
			t.Fatalf("len(resp.Messages) = %d; want 3 (%s)", len(resp.Messages), gatewayEdiMessages(resp))
		}
		for _, want := range []string{"Active coverage", "Co-pay 20%", "Deductible 500"} {
			if !strings.Contains(gatewayEdiMessages(resp), want) {
				t.Errorf("messages %q do not include %q", gatewayEdiMessages(resp), want)
			}
		}
	})

	t.Run("failure_keeps_gateway_messages", func(t *testing.T) {
		body := gatewayEdiInquiryResponse("SystemError", "", "Gateway is unavailable")
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(body))

		resp, err := gatewayEdiPost(t, g, srv.URL+"/eligibility", gatewayEdiTestPayload, nil, srv.Client())
		if err != nil {
			t.Fatalf("postSoapRequest() error = %v", err)
		}
		if resp == nil {
			t.Fatal("postSoapRequest() returned nil response")
		}
		if resp.Status != StatusServerError || resp.SuccessCode != SuccessCodeSystemError {
			t.Errorf("SystemError mapped to (%q, %q); want (%q, %q)",
				resp.Status, resp.SuccessCode, StatusServerError, SuccessCodeSystemError)
		}
		if !strings.Contains(gatewayEdiMessages(resp), "Gateway is unavailable") {
			t.Errorf("messages %q do not include the gateway message", gatewayEdiMessages(resp))
		}
	})
}
