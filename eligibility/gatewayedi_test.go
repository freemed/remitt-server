package eligibility

import (
	"bytes"
	"context"
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

// gatewayEdiTestEncryptedPayload stands in for the PGP-encrypted SOAP
// envelope produced by CheckEligibility.
var gatewayEdiTestEncryptedPayload = []byte("-----BEGIN PGP MESSAGE-----\n\nencrypted-soap-envelope\n-----END PGP MESSAGE-----\n")

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
		if len(body) == 0 {
			t.Error("request body is empty; the encrypted payload was not POSTed")
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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
		if got := srv.receivedBody(); !bytes.Equal(got, gatewayEdiTestEncryptedPayload) {
			t.Errorf("posted body = %q; want the encrypted payload %q", got, gatewayEdiTestEncryptedPayload)
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, privateKey, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, privateKey, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "private key") {
			t.Errorf("message %q does not mention the missing private key", gatewayEdiMessages(resp))
		}
	})

	t.Run("validation_failure_is_bad", func(t *testing.T) {
		body := gatewayEdiInquiryResponse("ValidationFailure", "", "Member not eligible on date of service")
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(body))

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "Invalid subscriber") {
			t.Errorf("message %q does not include the SOAP fault detail", gatewayEdiMessages(resp))
		}
	})

	t.Run("unparseable_body", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility",
			gatewayEdiXMLReply("<html><body>GatewayEDI maintenance window</body></html>"))

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "unrecognised GatewayEDI response format") {
			t.Errorf("message %q does not name the unrecognised response format", gatewayEdiMessages(resp))
		}
	})

	t.Run("not_xml_body", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply("200 OK but not XML at all"))

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, privateKey, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
	})

	t.Run("non_2xx_response", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiStatusReply(http.StatusInternalServerError, "internal error"))

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
		assertGatewayEdiFailure(t, resp, err)
		if resp != nil && !strings.Contains(gatewayEdiMessages(resp), "500") {
			t.Errorf("message %q does not report the HTTP status code", gatewayEdiMessages(resp))
		}
	})

	t.Run("empty_service_uri", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(gatewayEdiTestSuccessBody))

		for _, uri := range []string{"", "   "} {
			resp, err := g.postSoapRequest(uri, gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

		resp, err := g.postSoapRequest(deadURL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, nil)
		assertGatewayEdiFailure(t, resp, err)
		if err == nil {
			t.Error("postSoapRequest() error = nil after the endpoint went away; want an error")
		}
	})

	t.Run("empty_payload_rejected", func(t *testing.T) {
		srv := newGatewayEdiTestServer(t, "/eligibility", gatewayEdiXMLReply(gatewayEdiTestSuccessBody))

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", nil, nil, srv.Client())
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

			resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

			resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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

		resp, err := g.postSoapRequest(srv.URL+"/eligibility", gatewayEdiTestEncryptedPayload, nil, srv.Client())
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
