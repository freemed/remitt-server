package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/common"
)

// ---------------------------------------------------------------------------
// Parser API Registration Test
// ---------------------------------------------------------------------------

func TestParserApiRouteRegistered(t *testing.T) {
	if _, ok := common.ApiMap["parser"]; !ok {
		t.Error("expected ApiMap entry for 'parser'")
	}
}

// ---------------------------------------------------------------------------
// parseData Endpoint Tests
// ---------------------------------------------------------------------------

// TestParseData_RequiresAuth verifies the parse endpoint requires
// authentication.
func TestParseData_RequiresAuth(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return false })

	body := `{"plugin": "X12835Parser", "data": "ISA*00*..."}`
	req := httptest.NewRequest(http.MethodPost, "/api/parser/parse",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", rec.Code)
	}
}

// TestParseData_ValidRequest tests a valid parseData request with X12835Parser.
// This test does not require a database connection.
func TestParseData_ValidRequest(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	// Minimal valid X12 835 data
	rawData := "ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER      *230101*1200*U*00401*000000001*0*T*:~" +
		"GS*HP*SENDER*RECEIVER*20230101*1200*1*X*004010X091A1~" +
		"ST*835*0001~" +
		"BPR*C*100.00*C*ACH*CTX*01*999999999*DA*123456789*987654321**01*999999999*DA*123456789*20230101~" +
		"TRN*1*TRACE123*999999999~" +
		"SE*6*0001~" +
		"GE*1*1~" +
		"IEA*1*000000001~"

	body := `{"plugin": "X12835Parser", "data": "` + rawData + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/parser/parse",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		return
	}

	var resp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to unmarshal response: %v", err)
		return
	}
	if resp.Result == "" {
		t.Error("expected non-empty result")
	}

	// Verify the result is valid JSON (parsed by X12835Parser)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(resp.Result), &parsed); err != nil {
		t.Errorf("result is not valid JSON: %v", err)
	}
}

// TestParseData_X12Parser tests the X12 envelope parser.
func TestParseData_X12Parser(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	rawData := "ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER      *230101*1200*U*00401*000000001*0*T*:~" +
		"GS*HP*SENDER*RECEIVER*20230101*1200*1*X*004010X091A1~" +
		"ST*835*0001~" +
		"BPR*C*100.00*C*ACH~" +
		"SE*3*0001~" +
		"GE*1*1~" +
		"IEA*1*000000001~"

	body := `{"plugin": "X12Parser", "data": "` + rawData + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/parser/parse",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		return
	}

	var resp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to unmarshal response: %v", err)
		return
	}
	if resp.Result == "" {
		t.Error("expected non-empty result")
	}
}

// TestParseData_InvalidPlugin tests with a non-existent parser plugin name.
func TestParseData_InvalidPlugin(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	body := `{"plugin": "NonExistentParser", "data": "test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/parser/parse",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid plugin, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// TestParseData_InvalidJSON tests that malformed JSON returns 400.
func TestParseData_InvalidJSON(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	body := `{"plugin": "X12Parser", "data": `
	req := httptest.NewRequest(http.MethodPost, "/api/parser/parse",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid JSON, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// TestParseData_EmptyBody tests empty request body handling.
func TestParseData_EmptyBody(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodPost, "/api/parser/parse",
		strings.NewReader(""))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for empty body, got %d: %s",
			rec.Code, rec.Body.String())
	}
}
