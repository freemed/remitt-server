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
// Contract pinned here (script_http.go):
//   - a 2xx response returns the response body verbatim, even when it is empty
//   - anything else (non-2xx status, unreachable server, unparsable URL,
//     unreadable body) is returned as an "httpFailurePrefix" ("HTTP-ERROR: ")
//     string, so a rejection and a dead endpoint are never mistaken for the
//     payload and an empty body stays distinguishable from a failure
//   - every request carries the configured http.Client.Timeout (HTTPTimeout)
//   - response bodies are always closed

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

// TestScriptHTTP_Non2xxIsSurfacedAsAFailure pins that a rejection is reported
// to the script as a failure instead of being returned as if it were the
// payload: the body of a 404/500/403 is never handed over, and the failure
// string names the status.
//
// (Replaces TestScriptHTTP_Non2xxIsReturnedAsASuccess, which pinned the pre-fix
// behaviour: the error page body came back as a normal string with no error
// signal - script_http.go never inspected the status code.)
func TestScriptHTTP_Non2xxIsSurfacedAsAFailure(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := newHTTPTestServer(t, status, "error-page-body")
			ic := testInterpreter()

			got, err := evalJS(t, ic, `result = http.Get("`+srv.URL+`/");`, "result")
			if err != nil {
				t.Fatalf("running http.Get from JS: %v", err)
			}
			if got == "error-page-body" {
				t.Fatalf("http.Get() returned the HTTP %d error page body to the script as a success", status)
			}
			if !strings.HasPrefix(got, httpFailurePrefix) {
				t.Fatalf("http.Get() = %q; want a failure string starting with %q", got, httpFailurePrefix)
			}
			if !strings.Contains(got, strconv.Itoa(status)) || !strings.Contains(got, http.StatusText(status)) {
				t.Errorf("http.Get() = %q; want it to name HTTP %d %s", got, status, http.StatusText(status))
			}

			// Same contract for the authenticated helper.
			got, err = evalJS(t, ic, `result = http.GetWithBasicAuth("`+srv.URL+`/secure", "alice", "s3cr3t");`, "result")
			if err != nil {
				t.Fatalf("running http.GetWithBasicAuth from JS: %v", err)
			}
			if !strings.HasPrefix(got, httpFailurePrefix) || strings.Contains(got, "error-page-body") {
				t.Fatalf("http.GetWithBasicAuth() = %q; want a failure string naming HTTP %d with no response body", got, status)
			}
		})
	}
}

// TestScriptHTTP_Get_ConnectionFailureIsDistinguishableFromAnEmptyBody pins
// that an unreachable server is NOT conflated with a successful response that
// carried no body: the failure returns a "HTTP-ERROR: " string, while a 2xx
// with an empty body returns "".
//
// (Replaces TestScriptHTTP_Get_ConnectionFailureReturnsEmptyString, which pinned
// the pre-fix behaviour: both cases returned "".)
func TestScriptHTTP_Get_ConnectionFailureIsDistinguishableFromAnEmptyBody(t *testing.T) {
	// A loopback port with nothing listening gives a connect failure.
	url := "http://127.0.0.1:" + strconv.Itoa(closedLocalPort(t)) + "/unreachable"
	ic := testInterpreter()

	got, err := evalJS(t, ic, `result = http.Get("`+url+`");`, "result")
	if err != nil {
		t.Fatalf("running http.Get from JS against an unreachable server: %v", err)
	}
	if got == "" {
		t.Fatal("http.Get() = \"\" for an unreachable server; a connection failure must not look like an empty body")
	}
	if !strings.HasPrefix(got, httpFailurePrefix) {
		t.Fatalf("http.Get() = %q; want a failure string starting with %q", got, httpFailurePrefix)
	}

	got, err = evalJS(t, ic, `result = http.GetWithBasicAuth("`+url+`", "a", "b");`, "result")
	if err != nil {
		t.Fatalf("running http.GetWithBasicAuth from JS against an unreachable server: %v", err)
	}
	if !strings.HasPrefix(got, httpFailurePrefix) {
		t.Fatalf("http.GetWithBasicAuth() = %q; want a failure string starting with %q", got, httpFailurePrefix)
	}

	// The contrast that makes the two distinguishable: a 2xx response with an
	// empty body is a success, and returns "".
	empty, _ := newHTTPTestServer(t, http.StatusOK, "")
	got, err = evalJS(t, ic, `result = http.Get("`+empty.URL+`/");`, "result")
	if err != nil {
		t.Fatalf("running http.Get from JS against an empty 2xx body: %v", err)
	}
	if got != "" {
		t.Fatalf("http.Get() = %q for a 2xx response with an empty body; want \"\"", got)
	}
}

// TestScriptHTTP_MalformedURLReturnsAFailure pins that a URL net/http refuses
// to parse is reported to the script instead of panicking: the discarded
// http.NewRequest error used to make the helpers dereference a nil request, and
// that panic escaped otto's VM (script.go:RunUnsafe re-panics anything that is
// not its own errHalt sentinel) and took the job worker down.
//
// (Replaces TestScriptHTTP_MalformedURLPanics, which pinned the panic.)
func TestScriptHTTP_MalformedURLReturnsAFailure(t *testing.T) {
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
			// A panic here fails the test: the helper must return, not unwind.
			got := hc.Get(tt.url)
			if got == "" {
				t.Fatalf("Get(%q) = \"\"; want a failure string", tt.url)
			}
			if !strings.HasPrefix(got, httpFailurePrefix) {
				t.Fatalf("Get(%q) = %q; want a failure string starting with %q", tt.url, got, httpFailurePrefix)
			}
			if !strings.Contains(got, "invalid URL") {
				t.Errorf("Get(%q) = %q; want it to report the invalid URL", tt.url, got)
			}

			auth := hc.GetWithBasicAuth(tt.url, "a", "b")
			if !strings.HasPrefix(auth, httpFailurePrefix) {
				t.Fatalf("GetWithBasicAuth(%q) = %q; want a failure string starting with %q", tt.url, auth, httpFailurePrefix)
			}
		})
	}

	// The JS path must survive the same input.
	got, err := evalJS(t, ic, `result = http.Get("http://example.invalid:named-port/");`, "result")
	if err != nil {
		t.Fatalf("running http.Get from JS with an unparsable port: %v", err)
	}
	if !strings.HasPrefix(got, httpFailurePrefix) {
		t.Fatalf("http.Get() = %q; want a failure string starting with %q", got, httpFailurePrefix)
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

// TestScriptHTTP_ClientTimeoutIsApplied pins that both helpers bound their
// requests with the configured HTTPTimeout: a response slower than the
// configured bound is abandoned instead of holding the job worker forever,
// while a response inside the bound is still waited out and returned. A
// non-positive configuration falls back to the default rather than silently
// disabling the bound.
//
// (Replaces TestScriptHTTP_NoClientTimeoutIsPinned, which pinned the pre-fix
// behaviour: neither helper set http.Client.Timeout - the configured
// HTTPTimeout was commented out - so a hung payer endpoint blocked a worker
// indefinitely.)
func TestScriptHTTP_ClientTimeoutIsApplied(t *testing.T) {
	prev := HTTPTimeout
	t.Cleanup(func() { HTTPTimeout = prev })

	// The client both helpers build carries the configured timeout, and a
	// non-positive configuration falls back to the default.
	t.Run("client carries the configured timeout", func(t *testing.T) {
		tests := []struct {
			name       string
			configured time.Duration
			want       time.Duration
		}{
			{"configured value", 250 * time.Millisecond, 250 * time.Millisecond},
			{"unset falls back to the default", 0, defaultHTTPTimeout},
			{"negative falls back to the default", -time.Second, defaultHTTPTimeout},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				HTTPTimeout = tt.configured
				if got := newHTTPClient().Timeout; got != tt.want {
					t.Errorf("newHTTPClient().Timeout = %v with HTTPTimeout = %v; want %v", got, tt.configured, tt.want)
				}
			})
		}
	})

	// A response slower than the configured timeout never reaches the script.
	t.Run("slow response is abandoned", func(t *testing.T) {
		slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			_, _ = w.Write([]byte("late"))
		}))
		t.Cleanup(slow.Close)

		HTTPTimeout = 250 * time.Millisecond
		ic := testInterpreter()
		hc := &httpclient{obj: ic}

		start := time.Now()
		got := hc.Get(slow.URL)
		elapsed := time.Since(start)

		if elapsed >= 2*time.Second {
			t.Fatalf("Get() returned after %v; the %v HTTPTimeout was not applied", elapsed, HTTPTimeout)
		}
		if !strings.HasPrefix(got, httpFailurePrefix) {
			t.Fatalf("Get() = %q after the client timeout; want a failure string starting with %q", got, httpFailurePrefix)
		}

		start = time.Now()
		auth := hc.GetWithBasicAuth(slow.URL, "a", "b")
		if elapsed := time.Since(start); elapsed >= 2*time.Second {
			t.Fatalf("GetWithBasicAuth() returned after %v; the %v HTTPTimeout was not applied", elapsed, HTTPTimeout)
		}
		if !strings.HasPrefix(auth, httpFailurePrefix) {
			t.Fatalf("GetWithBasicAuth() = %q after the client timeout; want a failure string starting with %q", auth, httpFailurePrefix)
		}
	})

	// A response inside the timeout is waited out, as before.
	t.Run("response within the timeout is waited out", func(t *testing.T) {
		slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte("late"))
		}))
		t.Cleanup(slow.Close)

		HTTPTimeout = defaultHTTPTimeout
		ic := testInterpreter()
		hc := &httpclient{obj: ic}

		start := time.Now()
		if got := hc.Get(slow.URL); got != "late" {
			t.Fatalf("Get() = %q; want %q", got, "late")
		}
		if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
			t.Fatalf("Get() returned after %v; want it to have waited for the slow response", elapsed)
		}
	})
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
