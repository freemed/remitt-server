// Route-contract tests: these pin the HTTP method+path pairs the SHIPPED
// client (client/client.go) actually sends, and they exercise them through the
// real router (e.ServeHTTP) rather than by calling handler methods.
//
// They exist because the previous suite could not catch either mismatch:
// api_test.go called Api.PayloadResubmit directly (so a completely unregistered
// route broke nothing), and its ping test asserted the server's own
// POST verb instead of the GET the client sends. A 404/405 assertion here fails
// the way the client would fail in production.
//
// Request lines mirrored literally from client/client.go:
//
//	client.go:80   Ping:            GET  c.URL + "/api/ping/" + pingText        (pingText = "PING")
//	client.go:272  PayloadResubmit: GET  c.URL + fmt.Sprintf("/api/payload/resubmit/%d", id)
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

const (
	// clientPingMethod/clientPingPath reproduce client.Ping's request line for
	// pingText "PING"; clientPingText is the value it compares the body to.
	clientPingMethod = http.MethodGet
	clientPingPath   = "/api/ping/PING"
	clientPingText   = "PING"

	// clientResubmitMethod/clientResubmitRoute reproduce
	// client.PayloadResubmit's request line. The path parameter must be named
	// "id": api.PayloadResubmit reads it with common.ParamInt(c, "id").
	clientResubmitMethod = http.MethodGet
	clientResubmitRoute  = "/api/payload/resubmit/:id"
)

// clientResubmitPath formats the client's resubmit path for an id. Any is used
// so tests can drive a non-numeric id without bypassing the router.
func clientResubmitPath(id any) string {
	return "/api/payload/resubmit/" + fmt.Sprint(id)
}

// registeredRoutes returns the router's own route table as a set of
// "METHOD PATH" strings. Reading the table from the router is the point: it is
// what a live server dispatches on, so it catches a route that was never
// registered as well as one registered on the wrong verb.
func registeredRoutes(e *echo.Echo) map[string]bool {
	out := map[string]bool{}
	for _, r := range e.Router().Routes() {
		out[r.Method+" "+r.Path] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

// TestPayloadResubmit_RouteRegisteredForClientVerb is the regression test for
// the missing resubmit route: api/payload.go registered only
// g.POST("/", PayloadInsert), so Api.PayloadResubmit was unreachable over HTTP.
func TestPayloadResubmit_RouteRegisteredForClientVerb(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	if !registeredRoutes(e)[clientResubmitMethod+" "+clientResubmitRoute] {
		t.Fatalf("route %q is not registered; client.PayloadResubmit (client/client.go:272) cannot reach Api.PayloadResubmit",
			clientResubmitMethod+" "+clientResubmitRoute)
	}
}

// TestPing_RouteRegisteredForClientVerb pins GET /api/ping/:text, the verb
// client.Ping sends. POST is asserted too because it is retained.
func TestPing_RouteRegisteredForClientVerb(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })
	routes := registeredRoutes(e)

	if !routes[clientPingMethod+" /api/ping/:text"] {
		t.Errorf("route %q is not registered; client.Ping (client/client.go:80) cannot reach Api.Ping",
			clientPingMethod+" /api/ping/:text")
	}
	if !routes[http.MethodPost+" /api/ping/:text"] {
		t.Errorf("route %q is not registered; the previously published POST verb was dropped",
			http.MethodPost+" /api/ping/:text")
	}
}

// ---------------------------------------------------------------------------
// Ping: the client's verb answers
// ---------------------------------------------------------------------------

// TestPing_ClientVerbAnswers drives the client's exact request line and asserts
// the body is the JSON string client.Ping compares against ("PING"), so a
// mismatch would fail here the way it fails in the client.
func TestPing_ClientVerbAnswers(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(clientPingMethod, clientPingPath, nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s (client.Ping's request) got %d, want 200: %s",
			clientPingMethod, clientPingPath, rec.Code, rec.Body.String())
	}

	var got string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("client.Ping would fail to unmarshal %q: %v", rec.Body.String(), err)
	}
	if got != clientPingText {
		t.Errorf("body decoded to %q, client.Ping expects %q", got, clientPingText)
	}
}

// TestPing_POSTAliasStillAnswers keeps the retained compatibility verb honest.
func TestPing_POSTAliasStillAnswers(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(http.MethodPost, clientPingPath, nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s got %d, want 200: %s", clientPingPath, rec.Code, rec.Body.String())
	}
	var got string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got != clientPingText {
		t.Errorf("POST %s body = %q (err %v), want %q", clientPingPath, got, err, clientPingText)
	}
}

// ---------------------------------------------------------------------------
// Payload resubmit: the client's verb+path reaches the handler
// ---------------------------------------------------------------------------

// TestPayloadResubmit_ClientVerbReachesHandler drives the client's exact
// method+path through the router and proves the request landed in
// Api.PayloadResubmit, not in the router's 404/405 handlers:
//
//   - the response is 400, not 404 (no route) or 405 (wrong verb);
//   - the error text is strconv's, which only common.ParamInt(c, "id") inside
//     PayloadResubmit can produce -- "bad parameter" would mean the :id
//     parameter was not bound, i.e. the path shape was wrong;
//   - a control path that is not registered still 404s, so the 400 is not
//     coming from a catch-all.
//
// The id is parsed before any database access, so no DB is needed.
func TestPayloadResubmit_ClientVerbReachesHandler(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(clientResubmitMethod, clientResubmitPath("abc"), nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("%s %s (client.PayloadResubmit's request) got %d: the client's verb+path reaches no handler",
			clientResubmitMethod, clientResubmitPath("abc"), rec.Code)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%s %s got %d, want 400 from PayloadResubmit: %s",
			clientResubmitMethod, clientResubmitPath("abc"), rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "invalid syntax") {
		t.Errorf("error %q is not strconv's parse error from common.ParamInt(c, \"id\"); "+
			"the :id path parameter is probably not bound", body)
	}
	if strings.Contains(body, "bad parameter") {
		t.Errorf("common.ParamInt reported an unbound parameter: route path %q does not carry :id", clientResubmitRoute)
	}

	// Control: an unregistered sibling path must still 404, so the 400 above
	// came from the registered route.
	ctrl := httptest.NewRequest(clientResubmitMethod, "/api/payload/resubmitX/abc", nil)
	ctrl.SetBasicAuth("testuser", "testpass")
	ctrlRec := httptest.NewRecorder()
	e.ServeHTTP(ctrlRec, ctrl)
	if ctrlRec.Code != http.StatusNotFound {
		t.Errorf("control path got %d, want 404 (the 400 must come from the registered route)", ctrlRec.Code)
	}

	// Control: the other verb on the same path is not routed.
	post := httptest.NewRequest(http.MethodPost, clientResubmitPath("abc"), nil)
	post.SetBasicAuth("testuser", "testpass")
	postRec := httptest.NewRecorder()
	e.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s got %d, want 405 (resubmit is registered on the client's GET verb)",
			clientResubmitPath("abc"), postRec.Code)
	}
}

// TestPayloadResubmit_ClientNumericIDDispatch drives the client's real request
// (a numeric id) through the router. Without a database the handler panics in
// model.Queries (nil pool), which is itself proof of dispatch -- the test skips
// on that panic and fails only if routing refused the request first.
func TestPayloadResubmit_ClientNumericIDDispatch(t *testing.T) {
	e := setupTestServer(func(u, p string) bool { return true })

	req := httptest.NewRequest(clientResubmitMethod, clientResubmitPath(1), nil)
	req.SetBasicAuth("testuser", "testpass")
	rec := httptest.NewRecorder()

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		e.ServeHTTP(rec, req)
	}()

	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("%s %s (client.PayloadResubmit's request) got %d: the client's verb+path reaches no handler",
			clientResubmitMethod, clientResubmitPath(1), rec.Code)
	}
	if panicked != nil {
		t.Skipf("no DB in unit tests: handler reached the DB layer as expected (%v)", panicked)
	}
	if rec.Code != http.StatusOK {
		t.Logf("%s %s answered %d: %s", clientResubmitMethod, clientResubmitPath(1), rec.Code, rec.Body.String())
	}
}
