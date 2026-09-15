package transport

// script_http_test.go pins the JS http client contract (script_http.go) which
// is the only HTTP surface a Script transport offers to plugin scripts
// (registered on the otto VM as `http` by script.go:Initialize).
//
// Asserted here: the request method and headers each helper sends, the body it
// returns, and what happens on a non-2xx response, an unreachable server and a
// malformed URL. All requests go to net/http/httptest servers - no external
// endpoint is contacted.
//
// Behaviour pinned as buggy (not fixed, production code is untouched):
//   - a non-2xx response is returned as if it were a successful body: the
//     status code is never inspected and no error is signalled to the script
//   - a connection failure is indistinguishable from an empty body ("")
//   - a URL that net/http refuses to parse panics inside Get/GetWithBasicAuth
//     (the *http.Request error is discarded, then request.Header is dereferenced)
//   - response bodies are never closed

import (
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freemed/remitt-server/model"
)

// closedLocalPort returns a loopback port that nothing is listening on, so an
// HTTP request to it fails to connect without leaving the machine.
func closedLocalPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not a *net.TCPAddr", l.Addr())
	}
	if err := l.Close(); err != nil {
		t.Fatalf("closing loopback listener: %v", err)
	}
	return addr.Port
}

// httpRecorder captures what the scripted HTTP helpers actually sent.
type httpRecorder struct {
	method  string
	url     string
	headers http.Header
	body    string
}

func newHTTPTestServer(t *testing.T, status int, body string) (*httptest.Server, *httpRecorder) {
	t.Helper()
	rec := &httpRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(payload)
		}
		rec.method = r.Method
		rec.url = r.URL.String()
		rec.headers = r.Header.Clone()
		rec.body = string(payload)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func testInterpreter() *Interpreter {
	ic := NewInterpreter(model.UserModel{Username: "admin", Id: 1})
	return &ic
}

// evalJS runs a snippet on the interpreter's VM and returns the value of the
// named global the snippet assigned.
func evalJS(t *testing.T, ic *Interpreter, snippet, resultName string) (string, error) {
	t.Helper()
	if _, err := ic.vm.Run(snippet); err != nil {
		return "", err
	}
	v, err := ic.vm.Get(resultName)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

func TestScriptHTTP_Get_SendsGETWithUserAgentAndReturnsBody(t *testing.T) {
	srv, rec := newHTTPTestServer(t, http.StatusOK, "HELLO-FROM-SERVER")
	ic := testInterpreter()

	got, err := evalJS(t, ic, `result = http.Get("`+srv.URL+`/path?q=1");`, "result")
	if err != nil {
		t.Fatalf("running http.Get from JS: %v", err)
	}
	if got != "HELLO-FROM-SERVER" {
		t.Errorf("http.Get() returned %q; want the response body %q", got, "HELLO-FROM-SERVER")
	}
	if rec.method != http.MethodGet {
		t.Errorf("server saw method %q; want GET", rec.method)
	}
	if rec.url != "/path?q=1" {
		t.Errorf("server saw URL %q; want /path?q=1", rec.url)
	}
	if got, want := rec.headers.Get("User-Agent"), "upload-server/2.0"; got != want {
		t.Errorf("User-Agent = %q; want %q", got, want)
	}
	if got := rec.headers.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q; want none for http.Get", got)
	}
	if rec.body != "" {
		t.Errorf("request body = %q; want an empty GET body", rec.body)
	}
}

func TestScriptHTTP_GetWithBasicAuth_SendsBasicAuthHeader(t *testing.T) {
	srv, rec := newHTTPTestServer(t, http.StatusOK, "AUTHED")
	ic := testInterpreter()

	got, err := evalJS(t, ic, `result = http.GetWithBasicAuth("`+srv.URL+`/secure", "alice", "s3cr3t");`, "result")
	if err != nil {
		t.Fatalf("running http.GetWithBasicAuth from JS: %v", err)
	}
	if got != "AUTHED" {
		t.Errorf("http.GetWithBasicAuth() returned %q; want %q", got, "AUTHED")
	}
	if rec.method != http.MethodGet {
		t.Errorf("server saw method %q; want GET", rec.method)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cr3t"))
	if got := rec.headers.Get("Authorization"); got != wantAuth {
		t.Errorf("Authorization = %q; want %q", got, wantAuth)
	}
	if got, want := rec.headers.Get("User-Agent"), "remitt/0.9"; got != want {
		t.Errorf("User-Agent = %q; want %q", got, want)
	}
	if rec.body != "" {
		t.Errorf("request body = %q; want an empty GET body", rec.body)
	}
}

// TestScriptHTTP_HelpersAreExposedUnderGoCasedNames pins the JS surface a
// plugin script must use: otto exposes the Go method names verbatim, so the
// helpers are http.Get / http.GetWithBasicAuth / http.GoQuery - NOT the
// lowerCamelCase names (e.g. the Java bridge's webClient.getPage style, or
// http.get) that a script written for the Java transport would use. Calling the
// camelCase name throws "TypeError: ... is not a function" inside transport().
func TestScriptHTTP_HelpersAreExposedUnderGoCasedNames(t *testing.T) {
	ic := testInterpreter()

	got, err := evalJS(t, ic, `result = Object.getOwnPropertyNames(http).join(",");`, "result")
	if err != nil {
		t.Fatalf("inspecting the http object: %v", err)
	}
	if got != "Get,GetWithBasicAuth,GoQuery" {
		t.Errorf("http exposes %q; want \"Get,GetWithBasicAuth,GoQuery\"", got)
	}

	for _, call := range []string{
		`http.get("http://127.0.0.1:1/")`,
		`http.getWithBasicAuth("http://127.0.0.1:1/", "a", "b")`,
		`http.goQuery("<html></html>")`,
	} {
		_, err := ic.vm.Run(`result = ` + call + `;`)
		if err == nil {
			t.Fatalf("%s succeeded; want a TypeError for the lowerCamelCase name", call)
		}
		if !strings.Contains(err.Error(), "is not a function") {
			t.Fatalf("%s failed with %v; want a TypeError", call, err)
		}
	}
	t.Log("pinned behaviour: a script must call http.Get/http.GetWithBasicAuth (otto uses the Go method names verbatim); \"http.get\" is undefined (script_http.go:16,32)")
}

// TestScriptHTTP_Non2xxIsReturnedAsASuccess documents CURRENT behaviour: a 500
// (or 404) response body is handed to the script as the return value of
// http.get() with no error and no status information, so a plugin script cannot
// tell a successful submission from a rejection.
func TestScriptHTTP_Non2xxIsReturnedAsASuccess(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := newHTTPTestServer(t, status, "error-page-body")
			ic := testInterpreter()

			got, err := evalJS(t, ic, `result = http.Get("`+srv.URL+`/");`, "result")
			if err != nil {
				t.Fatalf("running http.Get from JS: %v", err)
			}
			if got != "error-page-body" {
				t.Fatalf("http.Get() = %q; want the error page body %q (current behaviour: status is never inspected)", got, "error-page-body")
			}
			t.Logf("pinned behaviour: HTTP %d returned to the script as a normal string with no error signal (script_http.go:16-30)", status)
		})
	}
}

// TestScriptHTTP_Get_ConnectionFailureReturnsEmptyString documents CURRENT
// behaviour: an unreachable server and an empty response body are
// indistinguishable - both give the script "".
func TestScriptHTTP_Get_ConnectionFailureReturnsEmptyString(t *testing.T) {
	// A loopback port with nothing listening gives a connect failure.
	url := "http://127.0.0.1:" + strconv.Itoa(closedLocalPort(t)) + "/unreachable"
	ic := testInterpreter()

	got, err := evalJS(t, ic, `result = http.Get("`+url+`");`, "result")
	if err != nil {
		t.Fatalf("running http.Get from JS against an unreachable server: %v", err)
	}
	if got != "" {
		t.Fatalf("http.Get() = %q; want \"\" (current behaviour: a connection failure returns an empty string)", got)
	}

	got, err = evalJS(t, ic, `result = http.GetWithBasicAuth("`+url+`", "a", "b");`, "result")
	if err != nil {
		t.Fatalf("running http.GetWithBasicAuth from JS against an unreachable server: %v", err)
	}
	if got != "" {
		t.Fatalf("http.getWithBasicAuth() = %q; want \"\"", got)
	}
	t.Log("pinned behaviour: connection failures are swallowed and returned as \"\" (script_http.go:24-27, 41-44)")
}

// TestScriptHTTP_MalformedURLPanics documents CURRENT behaviour: http.NewRequest
// returns a nil request for an unparsable URL, the error is discarded, and the
// following request.Header.Set panics - inside a Script transport that panic
// escapes otto's VM and takes the job worker down (script.go:RunUnsafe
// re-panics anything that is not its own errHalt sentinel).
func TestScriptHTTP_MalformedURLPanics(t *testing.T) {
	ic := testInterpreter()
	hc := &httpclient{obj: ic}

	tests := []struct {
		name string
		url  string
	}{
		{"control character in URL", "http://example.invalid/\x7f"},
		{"unparseable port", "http://example.invalid:named-port/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				caught := recover()
				if caught == nil {
					t.Fatalf("Get(%q) did not panic; current behaviour changed (script_http.go:21-22 discards the NewRequest error)", tt.url)
				}
				t.Logf("pinned behaviour: Get(%q) panics with %v (script_http.go:21-22)", tt.url, caught)
			}()
			_ = hc.Get(tt.url)
		})
	}
}

func TestScriptHTTP_GoQuery_ParsesDocuments(t *testing.T) {
	ic := testInterpreter()
	hc := &httpclient{obj: ic}

	body := []byte(`<html><body><div id="doc"><h1>Claim</h1><span class="status">ACCEPTED</span></div></body></html>`)
	doc := hc.GoQuery(body)
	if doc == nil {
		t.Fatal("GoQuery() = nil; want a parsed document")
	}
	if got := strings.TrimSpace(doc.Find("h1").Text()); got != "Claim" {
		t.Errorf("GoQuery().Find(\"h1\").Text() = %q; want %q", got, "Claim")
	}
	if got := strings.TrimSpace(doc.Find("span.status").Text()); got != "ACCEPTED" {
		t.Errorf("GoQuery().Find(\"span.status\").Text() = %q; want %q", got, "ACCEPTED")
	}
}

// TestScriptHTTP_GoQueryIsUnreachableFromJS documents CURRENT behaviour: the
// helper is installed on the VM as http.GoQuery, but its Go signature takes
// []byte, which otto cannot produce from a JavaScript string, so a script
// cannot parse a page it fetched with http.Get.
func TestScriptHTTP_GoQueryIsUnreachableFromJS(t *testing.T) {
	ic := testInterpreter()
	_, err := ic.vm.Run(`result = http.GoQuery("<html><body>x</body></html>");`)
	if err == nil {
		t.Fatal("http.GoQuery(string) succeeded from JS; the []byte parameter is now callable - update this test")
	}
	if !strings.Contains(err.Error(), "GoQuery") && !strings.Contains(err.Error(), "convert") && !strings.Contains(err.Error(), "type") {
		t.Fatalf("http.GoQuery(string) failed with %v; want a conversion error naming GoQuery", err)
	}
	t.Logf("pinned behaviour: http.GoQuery is not callable from JS: %v", err)
}

// TestScriptHTTP_NoClientTimeout documents CURRENT behaviour: the http.Client
// used by both helpers has no Timeout (the configured HTTPTimeout is commented
// out), so a hung payer endpoint blocks the job worker indefinitely.
func TestScriptHTTP_NoClientTimeoutIsPinned(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer slow.Close()

	ic := testInterpreter()
	hc := &httpclient{obj: ic}

	start := time.Now()
	if got := hc.Get(slow.URL); got != "late" {
		t.Fatalf("Get() = %q; want %q", got, "late")
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("Get() returned after %v; want it to have waited for the slow response", elapsed)
	}
	t.Log("pinned behaviour: neither helper sets http.Client.Timeout (script_http.go:18-20, 34-36); a slow endpoint is waited out with no upper bound")
}

func TestScriptHTTP_InterpreterExposesBothHelpers(t *testing.T) {
	ic := testInterpreter()
	if ctx := ic.GetContext(); ctx == nil {
		t.Fatal("Interpreter.GetContext() = nil; want a usable context")
	}
	for _, name := range []string{"http", "mail", "log"} {
		if _, err := ic.vm.Get(name); err != nil {
			t.Fatalf("VM has no %q binding: %v", name, err)
		}
	}
	// The mail helper is exposed under its Go method name too.
	got, err := evalJS(t, ic, `result = Object.getOwnPropertyNames(mail).join(",");`, "result")
	if err != nil {
		t.Fatalf("inspecting the mail object: %v", err)
	}
	if got != "SendMessage" {
		t.Errorf("mail exposes %q; want \"SendMessage\"", got)
	}
}
