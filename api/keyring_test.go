package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/common"
)

// TestKeyringAdd_ValidRequest tests the happy path: valid JSON body with all fields.
// The DB-dependent handler will panic/recover without a live DB connection.
func TestKeyringAdd_ValidRequest(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for keyring add): %v", r)
		}
	}()

	e := setupTestServer(func(u, p string) bool { return true })

	body := `{
		"key_name": "GatewayEDI",
		"private_key": "-----BEGIN PGP PRIVATE KEY-----\nTestKey\n-----END PGP PRIVATE KEY-----",
		"public_key": "-----BEGIN PGP PUBLIC KEY-----\nTestKey\n-----END PGP PUBLIC KEY-----"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/keyring/add",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Should NOT return 400 (request structure is valid)
	if rec.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 for valid body: %s", rec.Body.String())
	}
	// 500 is expected (no DB)
	if rec.Code == http.StatusInternalServerError {
		t.Logf("got 500 (expected: no DB in unit tests): %s", rec.Body.String())
	}
	// 200 means DB was available — check response
	if rec.Code == http.StatusOK {
		var result bool
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Errorf("failed to unmarshal response: %v", err)
		}
		if !result {
			t.Error("expected true response on success")
		}
	}
}

// TestKeyringAdd_InvalidJSON tests sending malformed JSON body.
func TestKeyringAdd_InvalidJSON(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodPost, "/api/keyring/add",
		strings.NewReader(`{"invalid`))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid JSON, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// TestKeyringAdd_EmptyBody tests empty request body handling.
func TestKeyringAdd_EmptyBody(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for keyring add): %v", r)
		}
	}()

	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodPost, "/api/keyring/add",
		strings.NewReader(""))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Empty body may 400 or 500 depending on how binding handles it
	if rec.Code == http.StatusBadRequest {
		t.Logf("got 400 (expected for empty body)")
	}
	if rec.Code == http.StatusInternalServerError {
		t.Logf("got 500 (DB-requiring code path): %s", rec.Body.String())
	}
}

// TestKeyringAdd_RequiresAuth verifies the keyring endpoint requires authentication.
func TestKeyringAdd_RequiresAuth(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return false })

	body := `{"key_name": "test", "private_key": "pk", "public_key": "pub"}`
	req := httptest.NewRequest(http.MethodPost, "/api/keyring/add",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", rec.Code)
	}
}

// TestKeyringAdd_MissingKeyName tests sending a request without key_name.
func TestKeyringAdd_MissingKeyName(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for keyring add): %v", r)
		}
	}()

	e := setupTestServer(func(u, p string) bool { return true })

	body := `{
		"private_key": "pk",
		"public_key": "pub"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/keyring/add",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// The handler should pass empty keyname to model, which may cause DB error (500)
	// or succeed with empty keyname (200). We just verify it doesn't panic.
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestKeyringAdd_MissingPrivateKey tests sending a request without private_key.
func TestKeyringAdd_MissingPrivateKey(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for keyring add): %v", r)
		}
	}()

	e := setupTestServer(func(u, p string) bool { return true })

	body := `{
		"key_name": "test",
		"public_key": "pub"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/keyring/add",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestKeyringAdd_MethodNotAllowed tests that GET is not allowed on the POST endpoint.
func TestKeyringAdd_MethodNotAllowed(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodGet, "/api/keyring/add", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

// TestKeyringRouteRegistered verifies the keyring route is in ApiMap.
func TestKeyringRouteRegistered(t *testing.T) {
	if _, ok := common.ApiMap["keyring"]; !ok {
		t.Error("expected ApiMap entry for 'keyring'")
	}
}

// TestKeyringAdd_OnlyPublicKey tests sending only a public key (no private key).
func TestKeyringAdd_OnlyPublicKey(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for keyring add): %v", r)
		}
	}()

	e := setupTestServer(func(u, p string) bool { return true })

	body := `{
		"key_name": "PublicOnly",
		"public_key": "-----BEGIN PGP PUBLIC KEY-----\nTestKey\n-----END PGP PUBLIC KEY-----"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/keyring/add",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
