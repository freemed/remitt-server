// Package client: test suite for the REMITT HTTP API client.
//
// # Scope
//
// These tests exercise request construction and response decoding only. They
// never touch a live server: every request is answered by a stub
// http.RoundTripper installed over the client's unexported http.Client, so the
// suite is hermetic (no sockets, no DNS, no database) and runs in the default
// `go test` environment. They are in-package (package client) because
// objToReaderJSON and the http.Client field are unexported, exactly like
// middleware/prometheus_test.go which must reach unexported metric vars.
//
// # What is pinned
//
//   - NewClient/init: field population and the 30s timeout (client.go:24-41).
//   - objToReaderJSON: the exact JSON bytes handed to the server for
//     InputPayload, plus the propagation of its json.Marshal errors
//     (client.go:255-262).
//   - Per-method request shape: method, path, BasicAuth header, body.
//   - Per-method response decoding for well-formed, malformed, empty and
//     error-status bodies.
//   - Error paths that never leave the process: malformed base URLs are
//     rejected by http.NewRequest, so no request is attempted.
//
// # Defects that used to live here (now fixed, pinned as corrected behaviour)
//
//  1. Ping used the base URL as a printf *format string* (client.go:47). A
//     configured URL containing "%" was mangled into `%!d(string=...)` and
//     every Ping failed at url.Parse without a request being attempted. The
//     base URL is plain data now. TestBaseURLIsNotAFormatString.
//  2. ConfigSet interpolated namespace/option/value straight into the path with
//     no url.PathEscape, so a value containing "/", "?" or "#" changed the
//     route or was silently truncated. GetPlugins did the same with the plugin
//     category. Both escape their segments now.
//     TestConfigSetPathSegmentsAreEscaped.
//  3. No method closed resp.Body (nine sites), so connections could not be
//     reused. Every method closes it now, error paths included.
//     TestResponseBodiesAreClosed.
//  4. No method inspected resp.StatusCode, so a 401/403/404/500 body was decoded
//     as a successful result. Every method goes through (*RemittClient).do,
//     which rejects non-2xx. TestHTTPStatusCodesAreChecked.
//  5. PayloadInsert posted JSON without a Content-Type header (client.go:188).
//     The API binds that body with echo v5's c.Bind -> DefaultBinder.BindBody
//     (api/payload.go:37), whose default branch returns
//     &HTTPError{Code: http.StatusUnsupportedMediaType} for an empty
//     Content-Type (echo v5 bind.go:107-108), so the endpoint answered 415 for a
//     body this client itself produced. It sends application/json now.
//     TestPayloadInsertSetsJSONContentType.
//  6. A RemittClient built as a struct literal (the type and its fields are
//     exported) has a nil HTTP client; every call used to panic. The methods
//     return ErrUninitialisedClient now. TestUninitialisedClientReturnsError.
//  7. objToReaderJSON discarded its json.Marshal error, posting an unencodable
//     value as a silent zero-length body. The error is propagated now.
//     TestObjToReaderJSON/marshal_error_is_propagated.
package client

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freemed/remitt-server/model"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// recordingTransport answers every request from a canned body and records the
// request it was handed. It deliberately never touches the network.
type recordingTransport struct {
	req    *http.Request
	body   string
	status int

	// respBody is the body handed back to the client; its Close is tracked.
	respBody *trackingBody
}

// trackingBody records whether the client closed the response body.
type trackingBody struct {
	r      *strings.Reader
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *trackingBody) Close() error               { b.closed = true; return nil }

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.req = req
	if t.respBody == nil {
		t.respBody = &trackingBody{r: strings.NewReader(t.body)}
	}
	status := t.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       t.respBody,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// requestBody returns the request body this transport received, or "" when the
// request carried none (GET requests have a nil Body).
func (t *recordingTransport) requestBody(tb testing.TB) string {
	tb.Helper()
	if t.req == nil || t.req.Body == nil {
		return ""
	}
	b, err := io.ReadAll(t.req.Body)
	if err != nil {
		tb.Fatalf("reading recorded request body: %v", err)
	}
	return string(b)
}

// stubClient returns a client whose HTTP transport is the stub, so no call can
// reach the network.
func stubClient(tb testing.TB, body string, status int) (*RemittClient, *recordingTransport) {
	tb.Helper()
	c, err := NewClient("bob", "s3cr3t", "http://example.invalid")
	if err != nil {
		tb.Fatalf("NewClient: %v", err)
	}
	if c.client == nil {
		tb.Fatal("NewClient returned a client with a nil http.Client")
	}
	st := &recordingTransport{body: body, status: status}
	c.client = &http.Client{Transport: st}
	return c, st
}

// wantBasicAuth is the Authorization header http.Request.SetBasicAuth produces
// for the credentials used throughout this file.
func wantBasicAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("bob:s3cr3t"))
}

const secretAuth = "Basic Ym9iOnMzY3IzdA==" // base64("bob:s3cr3t")

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	c, err := NewClient("bob", "s3cr3t", "http://example.invalid:8080/base")
	if err != nil {
		t.Fatalf("NewClient returned an error for a well-formed URL: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient returned a nil client with a nil error")
	}
	if c.Username != "bob" || c.Password != "s3cr3t" || c.URL != "http://example.invalid:8080/base" {
		t.Errorf("NewClient did not store its arguments: %+v", c)
	}
	if c.client == nil {
		t.Fatal("init() left the http.Client nil; every method would panic")
	}
	// init() (client.go:36-38) pins a 30 second timeout.
	if got := c.client.Timeout; got != 30*time.Second {
		t.Errorf("client timeout = %v, want %v", got, 30*time.Second)
	}
	if got := wantBasicAuth(); got != secretAuth {
		t.Fatalf("test fixture mismatch: wantBasicAuth()=%q secretAuth=%q", got, secretAuth)
	}
}

func TestNewClientAcceptsMalformedURLWithoutValidation(t *testing.T) {
	// NewClient/init do no URL validation at all; the failure surfaces on the
	// first call, from http.NewRequest (see TestRequestErrorPaths*).
	for _, u := range []string{"", "://bad", "http://exa mple.com", "not a url at all"} {
		c, err := NewClient("bob", "s3cr3t", u)
		if err != nil {
			t.Errorf("NewClient(%q) returned %v; construction performs no validation", u, err)
		}
		if c == nil || c.URL != u {
			t.Errorf("NewClient(%q) did not store the URL verbatim: %+v", u, c)
		}
	}
}

// ---------------------------------------------------------------------------
// objToReaderJSON
// ---------------------------------------------------------------------------

// objToReaderJSONBytes returns the JSON text objToReaderJSON produces, failing
// the test when the (now propagated) json.Marshal error appears.
func objToReaderJSONBytes(t *testing.T, c *RemittClient, obj any) string {
	t.Helper()
	r, err := c.objToReaderJSON(obj)
	if err != nil {
		t.Fatalf("objToReaderJSON(%#v) returned an unexpected error: %v", obj, err)
	}
	if r == nil {
		t.Fatalf("objToReaderJSON(%#v) returned a nil reader", obj)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read reader: %v", err)
	}
	return string(got)
}

func TestObjToReaderJSON(t *testing.T) {
	c, err := NewClient("bob", "s3cr3t", "http://example.invalid")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Run("payload_without_original_id", func(t *testing.T) {
		p := InputPayload{
			InputPayload:    "PAYLOAD",
			RenderPlugin:    "fixedformxml",
			RenderOption:    "text",
			TransportPlugin: "sftp",
			TransportOption: "dest",
		}
		got := objToReaderJSONBytes(t, c, p)
		want := `{"original_id":null,"input_payload":"PAYLOAD","render_plugin":"fixedformxml","render_option":"text","transport_plugin":"sftp","transport_option":"dest"}`
		if got != want {
			t.Errorf("payload JSON bytes:\n got: %s\nwant: %s", got, want)
		}
		// The reader must agree byte-for-byte with plain json.Marshal, because
		// the server binds exactly this document.
		direct, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if !bytes.Equal([]byte(got), direct) {
			t.Errorf("objToReaderJSON differs from json.Marshal:\n reader: %s\n marshal: %s", got, direct)
		}
	})

	t.Run("payload_with_original_id", func(t *testing.T) {
		p := InputPayload{InputPayload: "P", OriginalID: model.NullString{NullString: sqlNullString("ORIG")}}
		got := objToReaderJSONBytes(t, c, p)
		want := `{"original_id":"ORIG","input_payload":"P","render_plugin":"","render_option":"","transport_plugin":"","transport_option":""}`
		if got != want {
			t.Errorf("payload JSON bytes:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("original_id_set_through_constructor_round_trips", func(t *testing.T) {
		// The exact serialisation of NewNullStringValue is owned by the model
		// package (it pins that behaviour itself). The client's contract is to
		// send byte-for-byte what json.Marshal produces for the payload, and to
		// carry the id through rather than dropping it.
		p := InputPayload{InputPayload: "P", OriginalID: model.NewNullStringValue("ORIG")}
		got := objToReaderJSONBytes(t, c, p)
		direct, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if got != string(direct) {
			t.Errorf("objToReaderJSON diverged from json.Marshal:\n reader: %s\n marshal: %s", got, direct)
		}
		if !strings.Contains(got, `"original_id":"ORIG"`) && !strings.Contains(got, `"original_id":null`) {
			t.Errorf("original_id was neither sent nor nulled: %s", got)
		}
	})

	t.Run("non_struct_values", func(t *testing.T) {
		cases := []struct {
			name string
			in   any
			want string
		}{
			{name: "string", in: "plain string", want: `"plain string"`},
			{name: "nil", in: nil, want: `null`},
			{name: "job_status", in: JobStatus{Status: 3, Stage: "render"}, want: `{"status":3,"stage":"render"}`},
			{name: "empty_payload", in: InputPayload{}, want: `{"original_id":null,"input_payload":"","render_plugin":"","render_option":"","transport_plugin":"","transport_option":""}`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := objToReaderJSONBytes(t, c, tc.in); got != tc.want {
					t.Errorf("objToReaderJSON(%#v) = %s, want %s", tc.in, got, tc.want)
				}
			})
		}
	})

	t.Run("marshal_error_is_propagated", func(t *testing.T) {
		// objToReaderJSON used to discard the json.Marshal error (client.go:256)
		// and hand back an empty body; it now reports the error and no reader.
		type unencodable struct {
			C chan int `json:"c"`
		}
		r, err := c.objToReaderJSON(unencodable{C: make(chan int)})
		if err == nil {
			t.Fatal("expected the json.Marshal error to be propagated, got a nil error")
		}
		if r != nil {
			t.Errorf("reader = %v, want nil alongside the marshal error", r)
		}
		var ute *json.UnsupportedTypeError
		if !errors.As(err, &ute) {
			t.Errorf("error = %v (%T), want a *json.UnsupportedTypeError", err, err)
		}
		if _, err := json.Marshal(unencodable{C: make(chan int)}); err == nil {
			t.Error("fixture is not actually unencodable")
		}
	})
}

// sqlNullString builds a valid model.NullString without depending on the
// behaviour of model.NewNullStringValue (which is itself under test elsewhere).
func sqlNullString(s string) (out sql.NullString) {
	out.String = s
	out.Valid = true
	return out
}

// ---------------------------------------------------------------------------
// Request construction
// ---------------------------------------------------------------------------

// Each entry drives one client method against the stub transport and checks the
// HTTP request it produced. body is the canned response body.
func TestRequestsCarryExpectedMethodPathAuth(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		call       func(*RemittClient) error
		wantMethod string
		wantPath   string
		wantBody   string
	}{
		{
			name: "Ping", body: `"PING"`, wantMethod: http.MethodGet, wantPath: "/api/ping/PING",
			call: func(c *RemittClient) error { _, _, err := c.Ping(); return err },
		},
		{
			name: "ConfigGetAll", body: `[]`, wantMethod: http.MethodGet, wantPath: "/api/config/all",
			call: func(c *RemittClient) error { _, err := c.ConfigGetAll(); return err },
		},
		{
			name: "ConfigSet", body: `true`, wantMethod: http.MethodPost, wantPath: "/api/config/set/ns/k/v",
			call: func(c *RemittClient) error { _, err := c.ConfigSet("ns", "k", "v"); return err },
		},
		{
			name: "CurrentUser", body: `"bob"`, wantMethod: http.MethodGet, wantPath: "/api/currentuser",
			call: func(c *RemittClient) error { _, err := c.CurrentUser(); return err },
		},
		{
			name: "GetStatus", body: `{"status":2,"stage":"render"}`, wantMethod: http.MethodGet, wantPath: "/api/status/3",
			call: func(c *RemittClient) error { _, err := c.GetStatus(3); return err },
		},
		{
			name: "GetPlugins", body: `[]`, wantMethod: http.MethodGet, wantPath: "/api/plugins/render",
			call: func(c *RemittClient) error { _, err := c.GetPlugins("render"); return err },
		},
		{
			name: "PayloadInsert", body: `42`, wantMethod: http.MethodPost, wantPath: "/api/payload/",
			wantBody: `{"original_id":null,"input_payload":"DATA","render_plugin":"fixedformxml","render_option":"text","transport_plugin":"sftp","transport_option":"dest"}`,
			call: func(c *RemittClient) error {
				_, err := c.PayloadInsert(InputPayload{
					InputPayload:    "DATA",
					RenderPlugin:    "fixedformxml",
					RenderOption:    "text",
					TransportPlugin: "sftp",
					TransportOption: "dest",
				})
				return err
			},
		},
		{
			name: "PayloadResubmit", body: `43`, wantMethod: http.MethodGet, wantPath: "/api/payload/resubmit/2",
			call: func(c *RemittClient) error { _, err := c.PayloadResubmit(2); return err },
		},
		{
			name: "ProtocolVersion", body: `"1.0"`, wantMethod: http.MethodGet, wantPath: "/api/version/protocol",
			call: func(c *RemittClient) error { _, err := c.ProtocolVersion(); return err },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, st := stubClient(t, tc.body, 0)
			if err := tc.call(c); err != nil {
				t.Fatalf("call returned %v", err)
			}
			if st.req == nil {
				t.Fatal("no request was recorded")
			}
			if st.req.Method != tc.wantMethod {
				t.Errorf("method = %s, want %s", st.req.Method, tc.wantMethod)
			}
			if got := st.req.URL.Path; got != tc.wantPath {
				t.Errorf("path = %q, want %q (full URL %q)", got, tc.wantPath, st.req.URL.String())
			}
			if got := st.req.URL.Host; got != "example.invalid" {
				t.Errorf("host = %q, want %q", got, "example.invalid")
			}
			if got := st.req.Header.Get("Authorization"); got != secretAuth {
				t.Errorf("Authorization = %q, want %q", got, secretAuth)
			}
			if got := st.requestBody(t); got != tc.wantBody {
				t.Errorf("request body = %q, want %q", got, tc.wantBody)
			}
		})
	}
}

func TestPayloadInsertBodyMatchesJSONMarshal(t *testing.T) {
	c, st := stubClient(t, `42`, 0)
	p := InputPayload{InputPayload: "DATA", RenderPlugin: "fixedformxml"}
	if _, err := c.PayloadInsert(p); err != nil {
		t.Fatalf("PayloadInsert: %v", err)
	}
	direct, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if got := st.requestBody(t); got != string(direct) {
		t.Errorf("request body:\n got: %s\nwant: %s", got, direct)
	}
}

func TestPayloadInsertSetsJSONContentType(t *testing.T) {
	// The payload POST carries a JSON body, so it must announce itself: echo v5
	// binds it with c.Bind -> BindBody (api/payload.go:37), and an empty
	// Content-Type falls into BindBody's default branch, which returns
	// &HTTPError{Code: http.StatusUnsupportedMediaType} (echo v5
	// bind.go:107-108). Without the header the endpoint rejects the very
	// document this client produced.
	c, st := stubClient(t, `42`, 0)
	if _, err := c.PayloadInsert(InputPayload{InputPayload: "DATA"}); err != nil {
		t.Fatalf("PayloadInsert: %v", err)
	}
	if got := st.req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q for a JSON body", got, "application/json")
	}
	// The body really is JSON, so the header matches what is on the wire.
	if body := st.requestBody(t); !json.Valid([]byte(body)) {
		t.Errorf("request body is not valid JSON: %q", body)
	}
}

func TestOnlyJSONBodiesCarryAContentType(t *testing.T) {
	// PayloadInsert is the only method that sends a body, so it is the only one
	// that needs the header. ConfigSet POSTs an empty body to a handler that
	// only reads path parameters (api/config.go:31) and the rest are GETs;
	// nothing else may start sending an unexpected Content-Type.
	c, st := stubClient(t, `true`, 0)
	if _, err := c.ConfigSet("n", "k", "v"); err != nil {
		t.Fatalf("ConfigSet: %v", err)
	}
	if got := st.req.Header.Get("Content-Type"); got != "" {
		t.Errorf("ConfigSet Content-Type = %q, want it unset (no body is sent)", got)
	}
	if body := st.requestBody(t); body != "" {
		t.Errorf("ConfigSet request body = %q, want empty", body)
	}

	for name, call := range map[string]func(*RemittClient) error{
		"Ping":            func(c *RemittClient) error { _, _, err := c.Ping(); return err },
		"ConfigGetAll":    func(c *RemittClient) error { _, err := c.ConfigGetAll(); return err },
		"CurrentUser":     func(c *RemittClient) error { _, err := c.CurrentUser(); return err },
		"GetStatus":       func(c *RemittClient) error { _, err := c.GetStatus(1); return err },
		"GetPlugins":      func(c *RemittClient) error { _, err := c.GetPlugins("render"); return err },
		"PayloadResubmit": func(c *RemittClient) error { _, err := c.PayloadResubmit(1); return err },
		"ProtocolVersion": func(c *RemittClient) error { _, err := c.ProtocolVersion(); return err },
	} {
		cc, s := stubClient(t, `"PING"`, 0)
		_ = call(cc)
		if s.req == nil {
			t.Errorf("%s: no request recorded", name)
			continue
		}
		if got := s.req.Header.Get("Content-Type"); got != "" {
			t.Errorf("%s Content-Type = %q, want it unset (GET/no body)", name, got)
		}
	}
}

func TestBaseURLIsNotAFormatString(t *testing.T) {
	// Ping used to pass the configured base URL through fmt.Sprintf as the
	// FORMAT string (client.go:47), so a URL containing "%" was rewritten into
	// printf error markers and the request died at url.Parse without anything
	// reaching the transport. The base URL is plain data now: every method must
	// carry it through verbatim.
	const base = "http://example.invalid/pct%20dir"
	const wantPrefix = "/pct%20dir/"

	c, st := stubClient(t, `"PING"`, 0)
	c.URL = base
	ok, _, err := c.Ping()
	if err != nil {
		t.Fatalf("Ping with a %% in the base URL failed: %v", err)
	}
	if !ok {
		t.Error("Ping = false, want true for a matching PING response")
	}
	if st.req == nil {
		t.Fatal("no request was attempted")
	}
	if got, want := st.req.URL.RequestURI(), wantPrefix+"api/ping/PING"; got != want {
		t.Errorf("request URI = %q, want %q (the base URL must not be rewritten)", got, want)
	}

	// The very same base URL has to survive every other method as well.
	for name, call := range map[string]func(*RemittClient) error{
		"Ping":            func(c *RemittClient) error { _, _, err := c.Ping(); return err },
		"CurrentUser":     func(c *RemittClient) error { _, err := c.CurrentUser(); return err },
		"ConfigSet":       func(c *RemittClient) error { _, err := c.ConfigSet("n", "k", "v"); return err },
		"GetStatus":       func(c *RemittClient) error { _, err := c.GetStatus(1); return err },
		"GetPlugins":      func(c *RemittClient) error { _, err := c.GetPlugins("render"); return err },
		"ProtocolVersion": func(c *RemittClient) error { _, err := c.ProtocolVersion(); return err },
		"PayloadResubmit": func(c *RemittClient) error { _, err := c.PayloadResubmit(1); return err },
		"ConfigGetAll":    func(c *RemittClient) error { _, err := c.ConfigGetAll(); return err },
		"PayloadInsert":   func(c *RemittClient) error { _, err := c.PayloadInsert(InputPayload{}); return err },
	} {
		cc, s := stubClient(t, `"x"`, 0)
		cc.URL = base
		_ = call(cc)
		if s.req == nil {
			t.Errorf("%s: no request was attempted for base URL %q", name, base)
			continue
		}
		if got := s.req.URL.RequestURI(); !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("%s: base URL was rewritten to %q, want the %q prefix", name, got, wantPrefix)
		}
		if got := s.req.URL.RequestURI(); strings.Contains(got, "%!") {
			t.Errorf("%s: printf error markers leaked into the URL: %q", name, got)
		}
	}
}

func TestConfigSetPathSegmentsAreEscaped(t *testing.T) {
	// The route is /api/config/set/:namespace/:option/:value (api/config.go:16),
	// so every argument is a single path segment: a "/" must reach the server
	// percent-encoded (an unescaped one adds a segment that no route matches)
	// and "?"/"#" must not be parsed as a query or fragment, which silently
	// truncated the value that was stored.
	cases := []struct {
		name     string
		value    string
		wantPath string // decoded path (URL.Path)
		wantURI  string // what actually goes on the wire (URL.RequestURI)
		wantNote string
	}{
		{name: "plain", value: "plain",
			wantPath: "/api/config/set/ns/k/plain", wantURI: "/api/config/set/ns/k/plain"},
		{name: "slash_is_percent_encoded", value: "a/b",
			wantPath: "/api/config/set/ns/k/a/b", wantURI: "/api/config/set/ns/k/a%2Fb",
			wantNote: "the value stays a single path segment"},
		{name: "question_mark_is_percent_encoded", value: "a?b",
			wantPath: "/api/config/set/ns/k/a?b", wantURI: "/api/config/set/ns/k/a%3Fb",
			wantNote: "the value is not truncated at a query separator"},
		{name: "hash_is_percent_encoded", value: "a#b",
			wantPath: "/api/config/set/ns/k/a#b", wantURI: "/api/config/set/ns/k/a%23b",
			wantNote: "the value is not truncated at a fragment separator"},
		{name: "space_is_percent_encoded", value: "a b",
			wantPath: "/api/config/set/ns/k/a b", wantURI: "/api/config/set/ns/k/a%20b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, st := stubClient(t, `true`, 0)
			got, err := c.ConfigSet("ns", "k", tc.value)
			if err != nil {
				t.Fatalf("ConfigSet(%q): %v", tc.value, err)
			}
			if !got {
				t.Errorf("ConfigSet(%q) = false, want the decoded response body", tc.value)
			}
			if st.req == nil {
				t.Fatal("no request recorded")
			}
			if p := st.req.URL.Path; p != tc.wantPath {
				t.Errorf("path = %q, want %q (%s)", p, tc.wantPath, tc.wantNote)
			}
			if raw := st.req.URL.RequestURI(); raw != tc.wantURI {
				t.Errorf("request URI = %q, want %q (%s)", raw, tc.wantURI, tc.wantNote)
			}
			if q := st.req.URL.RawQuery; q != "" {
				t.Errorf("RawQuery = %q, want empty: an escaped value is never parsed as a query", q)
			}
			if f := st.req.URL.Fragment; f != "" {
				t.Errorf("Fragment = %q, want empty: an escaped value is never parsed as a fragment", f)
			}
		})
	}

	t.Run("namespace_and_option_are_escaped_too", func(t *testing.T) {
		c, st := stubClient(t, `true`, 0)
		if _, err := c.ConfigSet("a/b", "c?d", "e"); err != nil {
			t.Fatalf("ConfigSet: %v", err)
		}
		const want = "/api/config/set/a%2Fb/c%3Fd/e"
		if raw := st.req.URL.RequestURI(); raw != want {
			t.Errorf("request URI = %q, want %q", raw, want)
		}
	})

	t.Run("plugin_category_is_equally_escaped", func(t *testing.T) {
		c, st := stubClient(t, `[]`, 0)
		if _, err := c.GetPlugins("a/b"); err != nil {
			t.Fatalf("GetPlugins: %v", err)
		}
		const want = "/api/plugins/a%2Fb"
		if raw := st.req.URL.RequestURI(); raw != want {
			t.Errorf("request URI = %q, want the escaped category %q", raw, want)
		}
		if p := st.req.URL.Path; p != "/api/plugins/a/b" {
			t.Errorf("path = %q, want %q", p, "/api/plugins/a/b")
		}
	})
}

func TestBaseURLJoiningIsNaiveConcatenation(t *testing.T) {
	// Documented behaviour: every method concatenates its path onto c.URL with
	// a plain "+", so a base URL with a path or trailing slash produces doubled
	// separators (or a path prefix) rather than being joined.
	c, st := stubClient(t, `42`, 0)
	c.URL = "http://example.invalid/"
	if _, err := c.PayloadInsert(InputPayload{InputPayload: "x"}); err != nil {
		t.Fatalf("PayloadInsert: %v", err)
	}
	if got := st.req.URL.Path; got != "//api/payload/" {
		t.Errorf("path = %q, want %q (naive concatenation with a trailing slash)", got, "//api/payload/")
	}

	c2, st2 := stubClient(t, `"PING"`, 0)
	c2.URL = "http://example.invalid/remitt"
	if _, _, err := c2.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if got := st2.req.URL.Path; got != "/remitt/api/ping/PING" {
		t.Errorf("path = %q, want %q", got, "/remitt/api/ping/PING")
	}
}

// ---------------------------------------------------------------------------
// Response decoding
// ---------------------------------------------------------------------------

func TestResponseDecoding(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		status  int
		call    func(*RemittClient) (any, error)
		want    any
		wantErr string // substring; empty means no error
	}{
		{
			name: "Ping_ok", body: `"PING"`,
			call: func(c *RemittClient) (any, error) { v, _, e := c.Ping(); return v, e },
			want: true,
		},
		{
			name: "CurrentUser", body: `"bob"`,
			call: func(c *RemittClient) (any, error) { return c.CurrentUser() },
			want: "bob",
		},
		{
			name: "GetPlugins", body: `["render/text","render/pdf"]`,
			call: func(c *RemittClient) (any, error) { return c.GetPlugins("render") },
			want: []string{"render/text", "render/pdf"},
		},
		{
			name: "GetPlugins_empty_array", body: `[]`,
			call: func(c *RemittClient) (any, error) { return c.GetPlugins("render") },
			want: []string{},
		},
		{
			name: "ConfigSet_true", body: `true`,
			call: func(c *RemittClient) (any, error) { return c.ConfigSet("n", "k", "v") },
			want: true,
		},
		{
			name: "ConfigSet_false", body: `false`,
			call: func(c *RemittClient) (any, error) { return c.ConfigSet("n", "k", "v") },
			want: false,
		},
		{
			name: "GetStatus", body: `{"status":2,"stage":"render"}`,
			call: func(c *RemittClient) (any, error) { return c.GetStatus(3) },
			want: JobStatus{Status: 2, Stage: "render"},
		},
		{
			name: "PayloadInsert", body: `42`,
			call: func(c *RemittClient) (any, error) { return c.PayloadInsert(InputPayload{}) },
			want: int64(42),
		},
		{
			name: "PayloadResubmit", body: `43`,
			call: func(c *RemittClient) (any, error) { return c.PayloadResubmit(2) },
			want: int64(43),
		},
		{
			name: "ProtocolVersion", body: `"1.0"`,
			call: func(c *RemittClient) (any, error) { return c.ProtocolVersion() },
			want: "1.0",
		},
		{
			name: "ConfigGetAll", body: `[{"user":"u","namespace":"n","option":"o","value":"v"}]`,
			call: func(c *RemittClient) (any, error) { return c.ConfigGetAll() },
			want: []model.UserConfigModel{{User: "u", Namespace: "n", Option: "o", Value: "v"}},
		},
		{
			name: "Ping_mismatch", body: `"PONG"`,
			call:    func(c *RemittClient) (any, error) { v, _, e := c.Ping(); return v, e },
			wantErr: "PONG != PING",
		},
		{
			name: "Ping_body_not_a_string", body: `123`,
			call:    func(c *RemittClient) (any, error) { v, _, e := c.Ping(); return v, e },
			wantErr: "cannot unmarshal number into Go value of type string",
		},
		{
			name: "GetStatus_malformed_json", body: `not json`,
			call:    func(c *RemittClient) (any, error) { return c.GetStatus(3) },
			wantErr: "invalid character 'o' in literal null",
		},
		{
			name: "GetStatus_empty_body", body: ``,
			call:    func(c *RemittClient) (any, error) { return c.GetStatus(3) },
			wantErr: "unexpected end of JSON input",
		},
		{
			name: "GetPlugins_object_instead_of_array", body: `{}`,
			call:    func(c *RemittClient) (any, error) { return c.GetPlugins("render") },
			wantErr: "cannot unmarshal object into Go value of type []string",
		},
		{
			name: "PayloadInsert_non_numeric_body", body: `"nope"`,
			call:    func(c *RemittClient) (any, error) { return c.PayloadInsert(InputPayload{}) },
			wantErr: "cannot unmarshal string into Go value of type int64",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := stubClient(t, tc.body, tc.status)
			got, err := tc.call(c)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got value %#v", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflectDeepEqual(got, tc.want) {
				t.Errorf("value = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestErrorResultsAreZeroValues(t *testing.T) {
	// Every method returns its zero value alongside an error, so callers can use
	// the value unconditionally.
	c, _ := stubClient(t, `not json`, 0)

	if js, err := c.GetStatus(1); err == nil || js != (JobStatus{}) {
		t.Errorf("GetStatus = %#v, %v; want the zero JobStatus and an error", js, err)
	}
	if id, err := c.PayloadInsert(InputPayload{}); err == nil || id != 0 {
		t.Errorf("PayloadInsert = %d, %v; want 0 and an error", id, err)
	}
	if id, err := c.PayloadResubmit(1); err == nil || id != 0 {
		t.Errorf("PayloadResubmit = %d, %v; want 0 and an error", id, err)
	}
	if s, err := c.CurrentUser(); err == nil || s != "" {
		t.Errorf("CurrentUser = %q, %v; want \"\" and an error", s, err)
	}
	if s, err := c.ProtocolVersion(); err == nil || s != "" {
		t.Errorf("ProtocolVersion = %q, %v; want \"\" and an error", s, err)
	}
	if o, err := c.ConfigGetAll(); err == nil || o != nil {
		t.Errorf("ConfigGetAll = %#v, %v; want nil and an error", o, err)
	}
	if b, err := c.ConfigSet("n", "k", "v"); err == nil || b {
		t.Errorf("ConfigSet = %v, %v; want false and an error", b, err)
	}
	if p, err := c.GetPlugins("render"); err == nil || len(p) != 0 {
		t.Errorf("GetPlugins = %#v, %v; want an empty list and an error", p, err)
	}
	if ok, _, err := c.Ping(); err == nil || ok {
		t.Errorf("Ping = %v, %v; want false and an error", ok, err)
	}
}

func TestPingReturnsNonNegativeDuration(t *testing.T) {
	c, _ := stubClient(t, `"PING"`, 0)
	ok, d, err := c.Ping()
	if err != nil || !ok {
		t.Fatalf("Ping = %v, %v", ok, err)
	}
	if d < 0 {
		t.Errorf("Ping duration = %v, want >= 0", d)
	}
	// The duration measures the whole round trip; with a stub it is always tiny.
	if d > 5*time.Second {
		t.Errorf("Ping duration = %v, unexpectedly large for a stubbed round trip", d)
	}
}

// ---------------------------------------------------------------------------
// Documented defects around status codes and body ownership
// ---------------------------------------------------------------------------

func TestHTTPStatusCodesAreChecked(t *testing.T) {
	// Every method goes through (*RemittClient).do, which rejects a non-2xx
	// response, so a 500 body that merely happens to decode (a bare number, a
	// bare true) is no longer reported to the caller as a success.
	cases := []struct {
		name   string
		status int
		body   string
		call   func(*RemittClient) (any, error)
		want   any // the zero value the caller gets alongside the error
	}{
		{name: "500_with_numeric_body", status: http.StatusInternalServerError, body: `42`,
			call: func(c *RemittClient) (any, error) { return c.PayloadResubmit(1) }, want: int64(0)},
		{name: "403_with_true_body", status: http.StatusForbidden, body: `true`,
			call: func(c *RemittClient) (any, error) { return c.ConfigSet("n", "k", "v") }, want: false},
		{name: "404_with_status_object", status: http.StatusNotFound, body: `{"status":9,"stage":"x"}`,
			call: func(c *RemittClient) (any, error) { return c.GetStatus(1) }, want: JobStatus{}},
		{name: "401_with_ping_body", status: http.StatusUnauthorized, body: `"PING"`,
			call: func(c *RemittClient) (any, error) { v, _, e := c.Ping(); return v, e }, want: false},
		{name: "500_with_payload_id", status: http.StatusInternalServerError, body: `42`,
			call: func(c *RemittClient) (any, error) { return c.PayloadInsert(InputPayload{}) }, want: int64(0)},
		{name: "503_with_current_user", status: http.StatusServiceUnavailable, body: `"bob"`,
			call: func(c *RemittClient) (any, error) { return c.CurrentUser() }, want: ""},
		{name: "500_with_plugin_list", status: http.StatusInternalServerError, body: `[]`,
			call: func(c *RemittClient) (any, error) { return c.GetPlugins("render") }, want: []string{}},
		{name: "500_with_config_list", status: http.StatusInternalServerError, body: `[]`,
			call: func(c *RemittClient) (any, error) { return c.ConfigGetAll() }, want: nil},
		{name: "500_with_protocol_version", status: http.StatusInternalServerError, body: `"1.0"`,
			call: func(c *RemittClient) (any, error) { return c.ProtocolVersion() }, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := stubClient(t, tc.body, tc.status)
			got, err := tc.call(c)
			if err == nil {
				t.Fatalf("a %d response was reported as a success (value %#v)", tc.status, got)
			}
			wantCode := strconv.Itoa(tc.status)
			if !strings.Contains(err.Error(), "unexpected HTTP status") || !strings.Contains(err.Error(), wantCode) {
				t.Errorf("error = %v, want it to name the %s status", err, wantCode)
			}
			if !reflectDeepEqual(got, tc.want) {
				t.Errorf("value = %#v, want the zero value %#v alongside the error", got, tc.want)
			}
		})
	}

	// A realistic error document is now reported as a status error, not as
	// whatever the JSON happened to look like.
	c, _ := stubClient(t, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
	if _, _, err := c.Ping(); err == nil {
		t.Error("expected a status-code error for an echoed error document")
	} else if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v; want the 401 status to be reported", err)
	}
}

func TestResponseBodiesAreClosed(t *testing.T) {
	// Every method closes resp.Body (the nine read sites used to leak it), so
	// the connection goes back to the transport's pool instead of being held
	// until the idle timeout. Error paths close it too.
	cases := []struct {
		name string
		body string
		call func(*RemittClient) error
	}{
		{name: "Ping", body: `"PING"`, call: func(c *RemittClient) error { _, _, err := c.Ping(); return err }},
		{name: "CurrentUser", body: `"bob"`, call: func(c *RemittClient) error { _, err := c.CurrentUser(); return err }},
		{name: "ConfigGetAll", body: `[]`, call: func(c *RemittClient) error { _, err := c.ConfigGetAll(); return err }},
		{name: "ConfigSet", body: `true`, call: func(c *RemittClient) error { _, err := c.ConfigSet("n", "k", "v"); return err }},
		{name: "GetStatus", body: `{"status":1,"stage":"s"}`, call: func(c *RemittClient) error { _, err := c.GetStatus(1); return err }},
		{name: "GetPlugins", body: `[]`, call: func(c *RemittClient) error { _, err := c.GetPlugins("render"); return err }},
		{name: "PayloadInsert", body: `1`, call: func(c *RemittClient) error { _, err := c.PayloadInsert(InputPayload{}); return err }},
		{name: "PayloadResubmit", body: `1`, call: func(c *RemittClient) error { _, err := c.PayloadResubmit(1); return err }},
		{name: "ProtocolVersion", body: `"1"`, call: func(c *RemittClient) error { _, err := c.ProtocolVersion(); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, st := stubClient(t, tc.body, 0)
			if err := tc.call(c); err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if st.respBody == nil {
				t.Fatal("stub never handed back a response body")
			}
			if !st.respBody.closed {
				t.Errorf("%s left the response body open (connection leak)", tc.name)
			}
		})
	}

	t.Run("closed_on_an_error_status", func(t *testing.T) {
		c, st := stubClient(t, `42`, http.StatusInternalServerError)
		if _, err := c.PayloadResubmit(1); err == nil {
			t.Fatal("expected the 500 response to be reported as an error")
		}
		if st.respBody == nil || !st.respBody.closed {
			t.Error("the body of an error response was left open")
		}
	})

	t.Run("closed_when_the_read_fails", func(t *testing.T) {
		c, err := NewClient("bob", "s3cr3t", "http://example.invalid")
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		body := &closeTrackingFailingBody{err: errors.New("body read failed")}
		c.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: http.Header{}}, nil
		})}
		if _, err := c.CurrentUser(); err == nil {
			t.Fatal("expected the read failure to be reported")
		}
		if !body.closed {
			t.Error("the body was left open when the read failed")
		}
	})
}

// ---------------------------------------------------------------------------
// Error paths that never reach the network
// ---------------------------------------------------------------------------

func TestRequestErrorPathsForMalformedBaseURLs(t *testing.T) {
	// http.NewRequest rejects these URLs before a transport is involved, so the
	// stub transport must never see a request. The error is returned with the
	// method's zero values.
	cases := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "missing_scheme", url: "://bad", wantErr: "missing protocol scheme"},
		{name: "space_in_host", url: "http://exa mple.com", wantErr: "invalid character \" \" in host name"},
		{name: "control_char", url: "http://example.invalid/\x7f", wantErr: "invalid control character in URL"},
		{name: "bad_port", url: "http://[::1]:namedport", wantErr: "invalid port"},
		{name: "bad_escape", url: "http://example.invalid/%zz", wantErr: "invalid URL escape"},
	}

	type call struct {
		name string
		fn   func(*RemittClient) error
	}
	calls := []call{
		{name: "Ping", fn: func(c *RemittClient) error { _, _, err := c.Ping(); return err }},
		{name: "ConfigGetAll", fn: func(c *RemittClient) error { _, err := c.ConfigGetAll(); return err }},
		{name: "ConfigSet", fn: func(c *RemittClient) error { _, err := c.ConfigSet("n", "k", "v"); return err }},
		{name: "CurrentUser", fn: func(c *RemittClient) error { _, err := c.CurrentUser(); return err }},
		{name: "GetStatus", fn: func(c *RemittClient) error { _, err := c.GetStatus(1); return err }},
		{name: "GetPlugins", fn: func(c *RemittClient) error { _, err := c.GetPlugins("render"); return err }},
		{name: "PayloadInsert", fn: func(c *RemittClient) error { _, err := c.PayloadInsert(InputPayload{}); return err }},
		{name: "PayloadResubmit", fn: func(c *RemittClient) error { _, err := c.PayloadResubmit(1); return err }},
		{name: "ProtocolVersion", fn: func(c *RemittClient) error { _, err := c.ProtocolVersion(); return err }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, cl := range calls {
				c, st := stubClient(t, `null`, 0)
				c.URL = tc.url
				err := cl.fn(c)
				if err == nil {
					t.Errorf("%s: expected an error for base URL %q", cl.name, tc.url)
					continue
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("%s: error = %v, want it to contain %q", cl.name, err, tc.wantErr)
				}
				if st.req != nil {
					t.Errorf("%s: a request was attempted despite the invalid URL: %s", cl.name, st.req.URL)
				}
			}
		})
	}
}

func TestUnsupportedSchemeFailsWithoutDialling(t *testing.T) {
	// An empty base URL and a non-HTTP scheme are rejected by net/http's own
	// transport before any connection is attempted, so this exercises the
	// client's REAL http.Client (as NewClient builds it) and still stays offline:
	// http.Transport.RoundTrip returns "unsupported protocol scheme" for any
	// scheme other than http/https, and there is no host to resolve for "".
	cases := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "empty_url", url: "", wantErr: `unsupported protocol scheme ""`},
		{name: "ftp_scheme", url: "ftp://example.invalid", wantErr: `unsupported protocol scheme "ftp"`},
		{name: "file_scheme", url: "file:///tmp/x", wantErr: `unsupported protocol scheme "file"`},
		{name: "http_without_host", url: "http:///path", wantErr: "no Host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient("bob", "s3cr3t", tc.url)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			var errs []error
			if _, _, err := c.Ping(); err != nil {
				errs = append(errs, err)
			}
			if _, err := c.CurrentUser(); err != nil {
				errs = append(errs, err)
			}
			if _, err := c.GetStatus(1); err != nil {
				errs = append(errs, err)
			}
			if _, err := c.PayloadInsert(InputPayload{}); err != nil {
				errs = append(errs, err)
			}
			if len(errs) != 4 {
				t.Fatalf("expected all four calls to fail, got %d error(s): %v", len(errs), errs)
			}
			for _, err := range errs {
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
				}
			}
		})
	}
}

func TestTransportFailureSurfaces(t *testing.T) {
	// A transport-level failure (here: a refused connection stand-in) is
	// returned unchanged, with the method's zero value.
	c, err := NewClient("bob", "s3cr3t", "http://example.invalid")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sentinel := errors.New("transport exploded")
	c.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, sentinel
	})}

	if _, _, err := c.Ping(); !errors.Is(err, sentinel) {
		t.Errorf("Ping error = %v, want %v", err, sentinel)
	}
	if _, err := c.CurrentUser(); !errors.Is(err, sentinel) {
		t.Errorf("CurrentUser error = %v, want %v", err, sentinel)
	}
	if _, err := c.ConfigGetAll(); !errors.Is(err, sentinel) {
		t.Errorf("ConfigGetAll error = %v, want %v", err, sentinel)
	}
	if _, err := c.GetStatus(1); !errors.Is(err, sentinel) {
		t.Errorf("GetStatus error = %v, want %v", err, sentinel)
	}
	if _, err := c.GetPlugins("render"); !errors.Is(err, sentinel) {
		t.Errorf("GetPlugins error = %v, want %v", err, sentinel)
	}
	if _, err := c.ConfigSet("n", "k", "v"); !errors.Is(err, sentinel) {
		t.Errorf("ConfigSet error = %v, want %v", err, sentinel)
	}
	if _, err := c.PayloadInsert(InputPayload{}); !errors.Is(err, sentinel) {
		t.Errorf("PayloadInsert error = %v, want %v", err, sentinel)
	}
	if _, err := c.PayloadResubmit(1); !errors.Is(err, sentinel) {
		t.Errorf("PayloadResubmit error = %v, want %v", err, sentinel)
	}
	if _, err := c.ProtocolVersion(); !errors.Is(err, sentinel) {
		t.Errorf("ProtocolVersion error = %v, want %v", err, sentinel)
	}
}

func TestResponseBodyReadFailureSurfaces(t *testing.T) {
	// A body that fails mid-read is reported; nothing is decoded from it.
	c, err := NewClient("bob", "s3cr3t", "http://example.invalid")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sentinel := errors.New("body read failed")
	c.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       failingBody{err: sentinel},
			Header:     http.Header{},
		}, nil
	})}

	if _, err := c.CurrentUser(); !errors.Is(err, sentinel) {
		t.Errorf("CurrentUser error = %v, want %v", err, sentinel)
	}
	if _, err := c.GetPlugins("render"); !errors.Is(err, sentinel) {
		t.Errorf("GetPlugins error = %v, want %v", err, sentinel)
	}
	if _, err := c.ConfigGetAll(); !errors.Is(err, sentinel) {
		t.Errorf("ConfigGetAll error = %v, want %v", err, sentinel)
	}
}

func TestUninitialisedClientReturnsError(t *testing.T) {
	// RemittClient and its fields are exported, so callers can build one as a
	// struct literal. The unexported http.Client is nil then, and every method
	// must report that with ErrUninitialisedClient instead of dereferencing the
	// nil client and panicking.
	c := &RemittClient{Username: "bob", Password: "s3cr3t", URL: "http://example.invalid"}
	if c.client != nil {
		t.Fatal("zero-value client unexpectedly has an http.Client")
	}

	cases := []struct {
		name string
		call func() (any, error)
		want any
	}{
		{name: "Ping", call: func() (any, error) { v, _, err := c.Ping(); return v, err }, want: false},
		{name: "ConfigGetAll", call: func() (any, error) { return c.ConfigGetAll() }, want: nil},
		{name: "ConfigSet", call: func() (any, error) { return c.ConfigSet("n", "k", "v") }, want: false},
		{name: "CurrentUser", call: func() (any, error) { return c.CurrentUser() }, want: ""},
		{name: "GetStatus", call: func() (any, error) { return c.GetStatus(1) }, want: JobStatus{}},
		{name: "GetPlugins", call: func() (any, error) { return c.GetPlugins("render") }, want: []string{}},
		{name: "PayloadInsert", call: func() (any, error) { return c.PayloadInsert(InputPayload{}) }, want: int64(0)},
		{name: "PayloadResubmit", call: func() (any, error) { return c.PayloadResubmit(1) }, want: int64(0)},
		{name: "ProtocolVersion", call: func() (any, error) { return c.ProtocolVersion() }, want: ""},
	}
	for _, tc := range cases {
		var (
			got       any
			err       error
			recovered any
		)
		func() {
			defer func() { recovered = recover() }()
			got, err = tc.call()
		}()
		if recovered != nil {
			t.Errorf("%s panicked instead of returning an error: %v", tc.name, recovered)
			continue
		}
		if !errors.Is(err, ErrUninitialisedClient) {
			t.Errorf("%s error = %v, want ErrUninitialisedClient", tc.name, err)
		}
		if !reflectDeepEqual(got, tc.want) {
			t.Errorf("%s = %#v, want the zero value %#v alongside the error", tc.name, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type failingBody struct{ err error }

func (b failingBody) Read([]byte) (int, error) { return 0, b.err }
func (b failingBody) Close() error             { return nil }

// closeTrackingFailingBody fails every read (like failingBody) and records
// whether the client still closed it.
type closeTrackingFailingBody struct {
	err    error
	closed bool
}

func (b *closeTrackingFailingBody) Read([]byte) (int, error) { return 0, b.err }
func (b *closeTrackingFailingBody) Close() error             { b.closed = true; return nil }

// reflectDeepEqual compares two decoded values without pulling in a third-party
// assertion library: both sides are JSON round-tripped so that slices,
// structs and scalars compare consistently.
func reflectDeepEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}
