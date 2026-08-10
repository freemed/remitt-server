package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/validation"
)

// mockValidator is a simple mock Validator for testing.
type mockValidator struct{}

func (m *mockValidator) Validate(data []byte) (*validation.ValidationResponse, error) {
	return &validation.ValidationResponse{
		Status:   "success",
		Messages: []string{"Validation passed"},
	}, nil
}

func (m *mockValidator) SetContext(ctx context.Context) error {
	return nil
}

func init() {
	// Register mock validator so /validate endpoint works in tests.
	validation.RegisterValidator("org.remitt.plugin.validation.MockValidator",
		func() validation.Validator { return &mockValidator{} })
}

// ---------------------------------------------------------------------------
// Validation API Route Registration Tests
// ---------------------------------------------------------------------------

func TestValidationRouteRegistered(t *testing.T) {
	if _, ok := common.ApiMap["validation"]; !ok {
		t.Error("expected ApiMap entry for 'validation'")
	}
}

// ---------------------------------------------------------------------------
// Validation Auth Tests
// ---------------------------------------------------------------------------

func TestValidation_RequiresAuth(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return false })

	body := `{"plugin": "org.remitt.plugin.validation.MockValidator", "data": "test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/validation/validate",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Validation Request Tests
// ---------------------------------------------------------------------------

func TestValidation_ValidRequest(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	body := `{
		"plugin": "org.remitt.plugin.validation.MockValidator",
		"data": "ISA*00*...~GS*HC*...~ST*835*..."
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/validation/validate",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp validation.ValidationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status 'success', got %q", resp.Status)
	}
	if len(resp.Messages) != 1 || resp.Messages[0] != "Validation passed" {
		t.Errorf("expected messages ['Validation passed'], got %v", resp.Messages)
	}
}

func TestValidation_MissingPlugin(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	body := `{
		"plugin": "org.remitt.plugin.validation.Nonexistent",
		"data": "ISA*00*...~GS*HC*..."
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/validation/validate",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing plugin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestValidation_InvalidJSON(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodPost, "/api/validation/validate",
		strings.NewReader(`{"invalid`))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}
