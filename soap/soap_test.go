package soap

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

// setupSOAPServer creates an Echo test server with just the SOAP middleware.
// BasicAuth and LoadUserMiddleware are simulated so c.Get("user") returns "testuser".
func setupSOAPServer() *echo.Echo {
	e := echo.New()

	// Simulate BasicAuth + LoadUserMiddleware by injecting the user into context
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			username, _, ok := c.Request().BasicAuth()
			if !ok || username == "" {
				return echo.ErrUnauthorized
			}
			c.Set("user", username)
			c.Set("roles", []string{"admin"})
			return next(c)
		}
	})

	// SOAP middleware
	e.Use(Middleware())

	// Catch-all 404 for non-SOAP paths
	e.Any("/*", func(c *echo.Context) error {
		return c.String(http.StatusNotFound, "not found")
	})

	return e
}

// soapRequest builds a SOAP 1.1 envelope with the given body XML.
func soapRequest(operationXML string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    %s
  </soap:Body>
</soap:Envelope>`, operationXML)
}

// sendSOAP sends a SOAP request and returns the response.
func sendSOAP(t *testing.T, e *echo.Echo, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, ServicePath, strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// Operation dispatch tests
// ---------------------------------------------------------------------------

func TestSOAP_GetProtocolVersion(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:getProtocolVersion xmlns:ns2="http://server.remitt.org/"/>`)
	rec := sendSOAP(t, e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	verifySOAPResponse(t, rec.Body.Bytes(), "getProtocolVersion", "0.6")
}

func TestSOAP_GetCurrentUserName(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:getCurrentUserName xmlns:ns2="http://server.remitt.org/"/>`)
	rec := sendSOAP(t, e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	verifySOAPResponse(t, rec.Body.Bytes(), "getCurrentUserName", "testuser")
}

func TestSOAP_ChangePassword(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:changePassword xmlns:ns2="http://server.remitt.org/"><pw>newpass</pw></ns2:changePassword>`)
	rec := sendSOAP(t, e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	verifySOAPResponse(t, rec.Body.Bytes(), "changePassword", "true")
}

func TestSOAP_InsertPayload(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:insertPayload xmlns:ns2="http://server.remitt.org/">
		<originalId>EXT-001</originalId>
		<inputPayload>data</inputPayload>
		<renderPlugin>org.remitt.plugin.render.PreRenderedPlugin</renderPlugin>
		<renderOption>x12</renderOption>
		<transportPlugin>storefile</transportPlugin>
		<transportOption/>
	</ns2:insertPayload>`)
	rec := sendSOAP(t, e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	verifySOAPResponseContains(t, rec.Body.Bytes(), "insertPayloadResponse")
}

func TestSOAP_ResubmitPayload(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:resubmitPayload xmlns:ns2="http://server.remitt.org/"><originalPayloadId>42</originalPayloadId></ns2:resubmitPayload>`)
	rec := sendSOAP(t, e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	verifySOAPResponseContains(t, rec.Body.Bytes(), "42")
}

func TestSOAP_GetStatus(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:getStatus xmlns:ns2="http://server.remitt.org/"><jobId>123</jobId></ns2:getStatus>`)
	rec := sendSOAP(t, e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	verifySOAPResponseContains(t, rec.Body.Bytes(), "123")
}

func TestSOAP_GetBulkStatus(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:getBulkStatus xmlns:ns2="http://server.remitt.org/"><jobIds>1</jobIds><jobIds>2</jobIds><jobIds>3</jobIds></ns2:getBulkStatus>`)
	rec := sendSOAP(t, e, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	verifySOAPResponseContains(t, rec.Body.Bytes(), "1 2 3")
}

func TestSOAP_EligibilityOperations(t *testing.T) {
	e := setupSOAPServer()

	t.Run("getEligibility", func(t *testing.T) {
		body := soapRequest(`<ns2:getEligibility xmlns:ns2="http://server.remitt.org/"><request><plugin>DummyEligibility</plugin></request></ns2:getEligibility>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "SUCCESS")
	})

	t.Run("batchEligibilityCheck", func(t *testing.T) {
		body := soapRequest(`<ns2:batchEligibilityCheck xmlns:ns2="http://server.remitt.org/"><requests/></ns2:batchEligibilityCheck>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "1")
	})
}

func TestSOAP_PluginOperations(t *testing.T) {
	e := setupSOAPServer()

	t.Run("getPlugins", func(t *testing.T) {
		body := soapRequest(`<ns2:getPlugins xmlns:ns2="http://server.remitt.org/"><category>render</category></ns2:getPlugins>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "render")
	})

	t.Run("getPluginOptions", func(t *testing.T) {
		body := soapRequest(`<ns2:getPluginOptions xmlns:ns2="http://server.remitt.org/"><pluginclass>test</pluginclass><qualifyingoption>x12</qualifyingoption></ns2:getPluginOptions>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "test:x12")
	})
}

func TestSOAP_FileOperations(t *testing.T) {
	e := setupSOAPServer()

	t.Run("getFile", func(t *testing.T) {
		body := soapRequest(`<ns2:getFile xmlns:ns2="http://server.remitt.org/"><category>output</category><filename>test.txt</filename></ns2:getFile>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "b3V0cHV0L3Rlc3QudHh0")
	})

	t.Run("getOutputMonths", func(t *testing.T) {
		body := soapRequest(`<ns2:getOutputMonths xmlns:ns2="http://server.remitt.org/"><targetYear>2024</targetYear></ns2:getOutputMonths>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "2024")
	})
}

func TestSOAP_KeyringAndUser(t *testing.T) {
	e := setupSOAPServer()

	t.Run("addKeyToKeyring", func(t *testing.T) {
		body := soapRequest(`<ns2:addKeyToKeyring xmlns:ns2="http://server.remitt.org/"><keyname>test</keyname><privatekey>abc</privatekey><publickey>def</publickey></ns2:addKeyToKeyring>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "true")
	})

	t.Run("addRemittUser", func(t *testing.T) {
		body := soapRequest(`<ns2:addRemittUser xmlns:ns2="http://server.remitt.org/"><user><username>newuser</username></user></ns2:addRemittUser>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "true")
	})

	t.Run("listRemittUsers", func(t *testing.T) {
		body := soapRequest(`<ns2:listRemittUsers xmlns:ns2="http://server.remitt.org/"/>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "listRemittUsersResponse")
	})
}

func TestSOAP_ParseAndValidate(t *testing.T) {
	e := setupSOAPServer()

	t.Run("parseData", func(t *testing.T) {
		body := soapRequest(`<ns2:parseData xmlns:ns2="http://server.remitt.org/"><parserClass>test.Parser</parserClass><data>ISA*00*...</data></ns2:parseData>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "test.Parser")
	})

	t.Run("validatePayload", func(t *testing.T) {
		body := soapRequest(`<ns2:validatePayload xmlns:ns2="http://server.remitt.org/"><validatorClass>test.Validator</validatorClass><data>SVNBKjAw*...</data></ns2:validatePayload>`)
		rec := sendSOAP(t, e, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		verifySOAPResponseContains(t, rec.Body.Bytes(), "true")
	})
}

// ---------------------------------------------------------------------------
// Error handling tests
// ---------------------------------------------------------------------------

func TestSOAP_UnknownOperation(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:nonexistentOperation xmlns:ns2="http://server.remitt.org/"/>`)
	rec := sendSOAP(t, e, body)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Fault") {
		t.Error("expected SOAP Fault in response")
	}
	if !strings.Contains(rec.Body.String(), "Unknown operation") {
		t.Error("expected 'Unknown operation' message")
	}
}

func TestSOAP_InvalidXML(t *testing.T) {
	e := setupSOAPServer()
	req := httptest.NewRequest(http.MethodPost, ServicePath, strings.NewReader("not xml"))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Fault") {
		t.Error("expected SOAP Fault for invalid XML")
	}
}

func TestSOAP_RequiresAuth(t *testing.T) {
	e := setupSOAPServer()
	body := soapRequest(`<ns2:getProtocolVersion xmlns:ns2="http://server.remitt.org/"/>`)
	req := httptest.NewRequest(http.MethodPost, ServicePath, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	// No BasicAuth header
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Auth middleware should reject before SOAP layer gets it
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 401 or 500 for unauthenticated, got %d", rec.Code)
	}
}

func TestSOAP_NonSOAPPathPassthrough(t *testing.T) {
	e := setupSOAPServer()
	req := httptest.NewRequest(http.MethodGet, "/api/version/", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Should NOT be SOAP — should pass through to REST handler (404 from catch-all)
	if rec.Code == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "Fault") {
		t.Error("non-SOAP path should not be caught by SOAP middleware")
	}
}

// ---------------------------------------------------------------------------
// XML type tests
// ---------------------------------------------------------------------------

func TestExtractOperation(t *testing.T) {
	tests := []struct {
		xml  string
		want string
	}{
		{`<ns2:getProtocolVersion xmlns:ns2="http://server.remitt.org/"/>`, "getProtocolVersion"},
		{`<ns2:changePassword xmlns:ns2="http://server.remitt.org/"><pw>test</pw></ns2:changePassword>`, "changePassword"},
		{`<ns2:getStatus xmlns:ns2="http://server.remitt.org/"><jobId>1</jobId></ns2:getStatus>`, "getStatus"},
		{`<ns2:getEligibility xmlns:ns2="http://server.remitt.org/"><request/></ns2:getEligibility>`, "getEligibility"},
	}
	for _, tt := range tests {
		got := extractOperation([]byte(tt.xml))
		if got != tt.want {
			t.Errorf("extractOperation(%q) = %q; want %q", tt.xml, got, tt.want)
		}
	}
}

func TestXMLText(t *testing.T) {
	xml := `<ns2:getStatus xmlns:ns2="http://server.remitt.org/"><jobId>42</jobId></ns2:getStatus>`
	got := xmlText([]byte(xml), "jobId")
	if got != "42" {
		t.Errorf("xmlText(jobId) = %q; want %q", got, "42")
	}
}

func TestXMLTextAll(t *testing.T) {
	xml := `<ns2:getBulkStatus xmlns:ns2="http://server.remitt.org/"><jobIds>1</jobIds><jobIds>2</jobIds></ns2:getBulkStatus>`
	got := xmlTextAll([]byte(xml), "jobIds")
	if len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("xmlTextAll(jobIds) = %v; want [1 2]", got)
	}
}

func TestMarshalXML(t *testing.T) {
	env := Envelope{
		Body: Body{
			InnerXML: []byte(`<ns2:testResponse xmlns:ns2="test"/>`),
		},
	}
	data := marshalXML(env)
	if len(data) == 0 {
		t.Error("marshalXML returned empty bytes")
	}
}

func TestNewFaultEnvelope(t *testing.T) {
	env := newFaultEnvelope("Client", "test error")
	data := marshalXML(env)
	dataStr := string(data)
	if !strings.Contains(dataStr, "Fault") {
		t.Error("fault envelope should contain Fault element")
	}
	if !strings.Contains(dataStr, "test error") {
		t.Error("fault envelope should contain error message")
	}
}

// ---------------------------------------------------------------------------
// Dispatch table completeness
// ---------------------------------------------------------------------------

func TestDispatchTableAllOperations(t *testing.T) {
	// Verify all 22 operations from the old Java Service.java are registered
	expectedOps := []string{
		"getProtocolVersion",
		"changePassword",
		"getCurrentUserName",
		"insertPayload",
		"resubmitPayload",
		"getConfigValues",
		"setConfigValue",
		"getStatus",
		"getBulkStatus",
		"getPlugins",
		"getFile",
		"getPluginOptions",
		"getFileList",
		"getOutputMonths",
		"getOutputYears",
		"getEligibility",
		"batchEligibilityCheck",
		"parseData",
		"addKeyToKeyring",
		"addRemittUser",
		"listRemittUsers",
		"validatePayload",
	}

	for _, op := range expectedOps {
		if _, ok := dispatchTable[op]; !ok {
			t.Errorf("operation %q not found in SOAP dispatch table", op)
		}
	}

	if len(dispatchTable) != len(expectedOps) {
		t.Errorf("dispatch table has %d entries; expected %d", len(dispatchTable), len(expectedOps))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func verifySOAPResponse(t *testing.T, body []byte, operation, expectedValue string) {
	t.Helper()
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "xml version") && !strings.Contains(bodyStr, "<?xml") {
		t.Error("response should contain XML declaration")
	}
	if !strings.Contains(bodyStr, "Envelope") {
		t.Error("response should be a SOAP envelope")
	}
	if !strings.Contains(bodyStr, expectedValue) {
		t.Errorf("expected %q in SOAP response body:\n%s", expectedValue, bodyStr)
	}
}

func verifySOAPResponseContains(t *testing.T, body []byte, expected string) {
	t.Helper()
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Envelope") {
		t.Error("response should be a SOAP envelope")
	}
	if !strings.Contains(bodyStr, expected) {
		t.Errorf("expected %q in SOAP response body:\n%s", expected, bodyStr)
	}
}

// parseSOAPBody extracts the inner XML from a SOAP response body.
func parseSOAPBody(body []byte) string {
	return ""
}
