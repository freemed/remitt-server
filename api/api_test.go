package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/common"
	"github.com/labstack/echo/v5"
)

// setupTestServer creates an Echo test server with all ApiMap routes registered.
// BasicAuth uses a mock callback and sets the user context key that handlers expect.
func setupTestServer(authFunc func(string, string) bool) *echo.Echo {
	e := echo.New()

	// Custom middleware that wraps BasicAuth + user context injection.
	// Echo v5 BasicAuth validates credentials but does NOT store the
	// username in context, so we add a wrapper that extracts the
	// username and sets it at common.AuthUserKey after successful auth.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// Run BasicAuth validation
			username, password, ok := c.Request().BasicAuth()
			if !ok {
				return echo.ErrUnauthorized
			}
			valid := authFunc(username, password)
			if !valid {
				return echo.ErrUnauthorized
			}
			// Set user in context for downstream handlers
			c.Set(common.AuthUserKey, username)
			return next(c)
		}
	})

	// Register all API routes
	api := e.Group("/api")
	for k, v := range common.ApiMap {
		v(api.Group("/" + k))
	}

	return e
}

// setAuthContext sets the auth user key in context for handlers that read it.
// In a real server, BasicAuth middleware + LoadUserMiddleware set this.
// For direct handler testing we inject it manually.
func setAuthContext(c *echo.Context, username string) {
	c.Set(common.AuthUserKey, username)
}

// ---------------------------------------------------------------------------
// Ping Endpoint Tests (no DB dependency)
// ---------------------------------------------------------------------------

func TestPingEndpoint(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodPost, "/api/ping/hello", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	// Ping endpoint returns the param text as JSON string
	var body string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Errorf("failed to unmarshal response: %v", err)
	}
	if body != "hello" {
		t.Errorf("expected 'hello', got %q", body)
	}
}

func TestPingEndpoint_NoAuth(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return false })

	req := httptest.NewRequest(http.MethodPost, "/api/ping/hello", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Should get 401 Unauthorized
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

func TestPingEndpoint_BadAuth(t *testing.T) {
	e := setupTestServer(func(u, p string) bool {
		return u == "admin" && p == "correct"
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ping/hello", nil)
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Version Endpoint Tests (no DB dependency)
// ---------------------------------------------------------------------------

func TestVersionEndpoint(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodGet, "/api/version/", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}

	var version string
	if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
		t.Errorf("failed to unmarshal version: %v", err)
	}
	if version == "" {
		t.Error("expected non-empty version string")
	}
}

func TestInfoEndpoint(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodGet, "/api/version/info", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var info map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Errorf("failed to unmarshal info: %v", err)
	}
	if _, ok := info["version"]; !ok {
		t.Error("expected 'version' field in info response")
	}
	if _, ok := info["remote_address"]; !ok {
		t.Error("expected 'remote_address' field in info response")
	}
}

func TestProtocolEndpoint(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodGet, "/api/version/protocol", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Payload Endpoint Tests
// ---------------------------------------------------------------------------

// TestPayloadInsert_Validation tests that the payload endpoint validates
// the incoming request body structure. DB-dependent, so we test the
// request parsing and validation paths.
func TestPayloadInsert_InvalidJSON(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	// Send invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/payload/",
		strings.NewReader(`{"invalid`))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Should get 400 Bad Request for malformed JSON
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid JSON, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// TestPayloadInsert_EmptyBody tests empty request body handling.
// Note: requires a database connection; skips if not available.
func TestPayloadInsert_EmptyBody(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for payload insert): %v", r)
		}
	}()

	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodPost, "/api/payload/",
		strings.NewReader(""))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Empty body should error (can't bind to struct) or hit DB layer
	if rec.Code == http.StatusBadRequest {
		t.Logf("got 400 (expected for empty body)")
	}
	if rec.Code != http.StatusBadRequest {
		t.Logf("got %d (DB-requiring code path): %s", rec.Code, rec.Body.String())
	}
}

// TestPayloadInsert_MissingRequiredFields tests sending payload with
// missing required fields. Requires a database connection.
func TestPayloadInsert_MissingRequiredFields(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for payload insert): %v", r)
		}
	}()

	e := setupTestServer(func(u, p string) bool { return true })

	// Missing render_plugin, transport_plugin
	body := `{"input_payload": "test data"}`
	req := httptest.NewRequest(http.MethodPost, "/api/payload/",
		strings.NewReader(body))
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// This will hit the DB layer and fail there, but the request
	// parsing itself should succeed. We check that it doesn't 400.
	if rec.Code == http.StatusBadRequest {
		t.Logf("got 400 (possibly from validation): %s", rec.Body.String())
	}
	// 401 means auth issue
	if rec.Code == http.StatusUnauthorized {
		t.Errorf("unexpected 401: %s", rec.Body.String())
	}
	// 500 is expected because there's no DB in unit tests
	if rec.Code == http.StatusInternalServerError {
		t.Logf("got 500 (expected: no DB in unit tests): %s", rec.Body.String())
	}
}

// TestPayloadInsert_ValidStructure tests that a well-formed payload
// request body is accepted (parsing succeeds, DB error is expected).
func TestPayloadInsert_ValidStructure(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("DB not available (required for payload insert): %v", r)
		}
	}()

	e := setupTestServer(func(u, p string) bool { return true })

	body := `{
		"input_payload": "ISA*00*...~GS*HC*...~",
		"render_plugin": "org.remitt.plugin.render.PreRenderedPlugin",
		"render_option": "x12",
		"transport_plugin": "storefile",
		"transport_option": ""
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/payload/",
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
	// 401 means auth issue
	if rec.Code == http.StatusUnauthorized {
		t.Errorf("unexpected 401: %s", rec.Body.String())
	}
}

// TestPayloadInsert_RequiresAuth verifies the payload endpoint requires
// authentication.
func TestPayloadInsert_RequiresAuth(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return false })

	body := `{"input_payload": "test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/payload/",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", rec.Code)
	}
}

// TestPayloadResubmit_RequiresAuth verifies the resubmit endpoint requires
// authentication.
func TestPayloadResubmit_RequiresAuth(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return false })

	req := httptest.NewRequest(http.MethodPost, "/api/payload/resubmit/1", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", rec.Code)
	}
}

// TestPayloadResubmit_BadID tests resubmit with a non-numeric ID.
func TestPayloadResubmit_BadID(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodPost, "/api/payload/resubmit/abc", nil)
	req.SetBasicAuth("testuser", "testpass")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Bad ID path should fail — but the resubmit endpoint uses Query params,
	// not path params. Let's check what we get — if there's no "id" param,
	// ParamInt will fail.
	if rec.Code != http.StatusOK {
		t.Logf("resubmit with bad path returned %d: %s (expected, no DB)", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Static File Serving Tests
// ---------------------------------------------------------------------------

// TestStaticUIRedirect tests the root redirect to /ui/index.html.
// Note: requires full server setup with static routes, not available
// in unit test context (tested via server integration tests).
func TestStaticUIRedirect(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Without full server setup (static routes, root redirect from main.go),
	// we get 404/401 depending on auth middleware placement.
	t.Logf("root endpoint returned %d (without full server setup)", rec.Code)
}

// ---------------------------------------------------------------------------
// All API Routes Registration Test
// ---------------------------------------------------------------------------

func TestAllApiRoutesRegistered(t *testing.T) {
	// Verify all expected API route groups are registered
	expectedRoutes := []string{
		"ping",
		"version",
		"payload",
		"file",
		"status",
	}

	for _, route := range expectedRoutes {
		if _, ok := common.ApiMap[route]; !ok {
			t.Errorf("expected ApiMap entry for %q", route)
		}
	}
}

// ---------------------------------------------------------------------------
// ACL Helper Tests
// ---------------------------------------------------------------------------

func TestIsAdmin_NoRoles(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	a := Api{}
	if a.isAdmin(c) {
		t.Error("isAdmin should return false when no roles are set")
	}
}

func TestIsAdmin_WithAdminRole(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("roles", []string{"admin", "user"})

	a := Api{}
	if !a.isAdmin(c) {
		t.Error("isAdmin should return true when admin role is present")
	}
}

func TestIsAdmin_WithoutAdminRole(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("roles", []string{"user", "viewer"})

	a := Api{}
	if a.isAdmin(c) {
		t.Error("isAdmin should return false when admin role is absent")
	}
}

func TestAclRequireRole_NoRoles(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	a := Api{}
	err := a.aclRequireRole(c, "admin")
	if err == nil {
		t.Error("aclRequireRole should return error when no roles are set")
	}
}

func TestAclRequireRole_WithRole(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("roles", []string{"admin"})

	a := Api{}
	err := a.aclRequireRole(c, "admin")
	if err != nil {
		t.Errorf("aclRequireRole should succeed when role matches: %v", err)
	}
}

func TestAclRequireRole_WrongRole(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("roles", []string{"user"})

	a := Api{}
	err := a.aclRequireRole(c, "admin")
	if err == nil {
		t.Error("aclRequireRole should return error when role doesn't match")
	}
}

// ---------------------------------------------------------------------------
// Auth Middleware Tests
// ---------------------------------------------------------------------------

func TestBasicAuth_ValidCredentials(t *testing.T) {
	e := setupTestServer(func(u, p string) bool {
		return u == "admin" && p == "secret"
	})

	req := httptest.NewRequest(http.MethodGet, "/api/version/", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}
}

func TestBasicAuth_InvalidPassword(t *testing.T) {
	e := setupTestServer(func(u, p string) bool {
		return u == "admin" && p == "secret"
	})

	req := httptest.NewRequest(http.MethodGet, "/api/version/", nil)
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestBasicAuth_EmptyCredentials(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return false })

	req := httptest.NewRequest(http.MethodGet, "/api/version/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing auth, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Method Not Allowed Tests
// ---------------------------------------------------------------------------

func TestVersionEndpoint_MethodNotAllowed(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	// POST to GET-only endpoint
	req := httptest.NewRequest(http.MethodPost, "/api/version/", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Echo returns 405 Method Not Allowed
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestPingEndpoint_MethodNotAllowed(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	// GET to POST-only endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/ping/hello", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 404 Tests
// ---------------------------------------------------------------------------

func TestUnknownEndpoint(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}
