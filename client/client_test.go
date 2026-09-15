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
//     InputPayload, plus its silent swallow of json.Marshal errors
//     (client.go:255-257).
//   - Per-method request shape: method, path, BasicAuth header, body.
//   - Per-method response decoding for well-formed, malformed, empty and
//     error-status bodies.
//   - Error paths that never leave the process: malformed base URLs are
//     rejected by http.NewRequest, so no request is attempted.
//
// # Defects documented here (pinned, not fixed)
//
//  1. client.go:47 (and the same pattern at 74, 119, 143, 165, 188, 211, 234):
//     the base URL is used as a printf *format string* in the Sprintf-based
//     methods. A configured URL containing "%" is mangled into
//     `%!d(string=...)`/`%!s(MISSING)` and every such request fails at
//     url.Parse. TestBaseURLIsNotAFormatString.
//  2. client.go:97: ConfigSet interpolates namespace/option/value straight into
//     the path with no url.PathEscape, so a value containing "/", "?" or "#"
//     changes the route or is silently dropped. TestConfigSetPathSegmentsAreEscaped.
//  3. Every method reads resp.Body and never closes it (client.go:56, 83, 106,
//     128, 152, 174, 194, 220, 245). TestResponseBodiesAreClosed.
//  4. No method inspects resp.StatusCode, so a 401/403/404/500 body is decoded
//     as a successful result. TestHTTPStatusCodesAreChecked.
//  5. PayloadInsert posts JSON without a Content-Type header (client.go:188).
//     The API binds that body with echo v5's c.Bind -> DefaultBinder.BindBody
//     (api/payload.go:37), whose default branch returns
//     &HTTPError{Code: http.StatusUnsupportedMediaType} for an empty
//     Content-Type (echo v5 bind.go:107-108), so the endpoint answers 415/400
//     for a body this client itself produced. TestPayloadInsertSetsJSONContentType.
//  6. A RemittClient built as a struct literal (the type and its fields are
//     exported) has a nil HTTP client and panics on every call.
//     TestUninitialisedClientPanics.
package client

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
		got, err := io.ReadAll(c.objToReaderJSON(p))
		if err != nil {
			t.Fatalf("read reader: %v", err)
		}
		want := `{"original_id":null,"input_payload":"PAYLOAD","render_plugin":"fixedformxml","render_option":"text","transport_plugin":"sftp","transport_option":"dest"}`
		if string(got) != want {
			t.Errorf("payload JSON bytes:\n got: %s\nwant: %s", got, want)
		}
		// The reader must agree byte-for-byte with plain json.Marshal, because
		// the server binds exactly this document.
		direct, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if !bytes.Equal(got, direct) {
			t.Errorf("objToReaderJSON differs from json.Marshal:\n reader: %s\n marshal: %s", got, direct)
		}
	})

	t.Run("payload_with_original_id", func(t *testing.T) {
		p := InputPayload{InputPayload: "P", OriginalID: model.NullString{NullString: sqlNullString("ORIG")}}
		got, err := io.ReadAll(c.objToReaderJSON(p))
		if err != nil {
			t.Fatalf("read reader: %v", err)
		}
		want := `{"original_id":"ORIG","input_payload":"P","render_plugin":"","render_option":"","transport_plugin":"","transport_option":""}`
		if string(got) != want {
			t.Errorf("payload JSON bytes:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("original_id_set_through_constructor_marshals_null", func(t *testing.T) {
		// Documented defect: NewNullStringValue leaves Valid=false, so an id set
		// through it serialises as null and the server cannot see it.
		p := InputPayload{InputPayload: "P", OriginalID: model.NewNullStringValue("ORIG")}
		got, err := io.ReadAll(c.objToReaderJSON(p))
		if err != nil {
			t.Fatalf("read reader: %v", err)
		}
		if !strings.Contains(string(got), `"original_id":null`) {
			t.Errorf("expected a null original_id (Valid is never set by NewNullStringValue), got: %s", got)
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
				got, err := io.ReadAll(c.objToReaderJSON(tc.in))
				if err != nil {
					t.Fatalf("read reader: %v", err)
				}
				if string(got) != tc.want {
					t.Errorf("objToReaderJSON(%#v) = %s, want %s", tc.in, got, tc.want)
				}
			})
		}
	})

	t.Run("marshal_error_is_swallowed", func(t *testing.T) {
		// objToReaderJSON discards the json.Marshal error (client.go:256), so an
		// unencodable value becomes a zero-length body rather than an error.
		type unencodable struct {
			C chan int `json:"c"`
		}
		got, err := io.ReadAll(c.objToReaderJSON(unencodable{C: make(chan int)}))
		if err != nil {
			t.Fatalf("read reader: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected the swallowed marshal error to yield an empty body, got %q", got)
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
	// Documented defect: the payload POST carries a JSON body but no
	// Content-Type header. echo v5 binds it with c.Bind -> BindBody
	// (api/payload.go:37), and an empty Content-Type falls into BindBody's
	// default branch, which returns
	// &HTTPError{Code: http.StatusUnsupportedMediaType} (echo v5 bind.go:107-108).
	// So POST /api/payload/ from this client is rejected as an unsupported media
	// type even though it produced a perfectly well-formed JSON document.
	c, st := stubClient(t, `42`, 0)
	if _, err := c.PayloadInsert(InputPayload{InputPayload: "DATA"}); err != nil {
		t.Fatalf("PayloadInsert: %v", err)
	}
	if got := st.req.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q; a JSON body should be sent as application/json", got)
	}
	// The body really is JSON, so the header is a plain omission.
	if body := st.requestBody(t); !json.Valid([]byte(body)) {
		t.Errorf("request body is not valid JSON: %q", body)
	}
}

func TestBaseURLIsNotAFormatString(t *testing.T) {
	// Documented defect: Ping passes the configured base URL through
	// fmt.Sprintf as the FORMAT string (client.go:47), so a URL containing "%"
	// is rewritten into printf error markers and the request dies at
	// url.Parse. The other methods concatenate the URL outside the Sprintf
	// (client.go:97, 143, 165, 211) or do not format it at all (119, 188, 234),
	// which is why the very same base URL works for them - that contrast is
	// asserted below to prove the cause is the Sprintf and not the URL.
	const base = "http://example.invalid/pct%20dir"

	c, st := stubClient(t, `"PING"`, 0)
	c.URL = base
	ok, _, err := c.Ping()
	if err == nil {
		t.Fatalf("Ping with a %% in the base URL unexpectedly succeeded (ok=%v)", ok)
	}
	if !strings.Contains(err.Error(), "%!") || !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("expected the printf-mangled URL in the error, got: %v", err)
	}
	if st.req != nil {
		t.Errorf("no request should have been attempted, got %s", st.req.URL)
	}

	// The same base URL is untouched by every method that does not feed it to
	// Sprintf as a format string.
	for name, call := range map[string]func(*RemittClient) error{
		"CurrentUser":     func(c *RemittClient) error { _, err := c.CurrentUser(); return err },
		"ConfigSet":       func(c *RemittClient) error { _, err := c.ConfigSet("n", "k", "v"); return err },
		"GetStatus":       func(c *RemittClient) error { _, err := c.GetStatus(1); return err },
		"GetPlugins":      func(c *RemittClient) error { _, err := c.GetPlugins("render"); return err },
		"ProtocolVersion": func(c *RemittClient) error { _, err := c.ProtocolVersion(); return err },
		"PayloadResubmit": func(c *RemittClient) error { _, err := c.PayloadResubmit(1); return err },
		"ConfigGetAll":    func(c *RemittClient) error { _, err := c.ConfigGetAll(); return err },
	} {
		cc, s := stubClient(t, `"x"`, 0)
		cc.URL = base
		_ = call(cc)
		if s.req == nil {
			t.Errorf("%s: no request was attempted for base URL %q", name, base)
			continue
		}
		if !strings.HasPrefix(s.req.URL.RequestURI(), "/pct%20dir/") {
			t.Errorf("%s: base URL was rewritten to %q", name, s.req.URL.RequestURI())
		}
	}
}

func TestConfigSetPathSegmentsAreEscaped(t *testing.T) {
	// Documented defect: ConfigSet interpolates the namespace, option and value
	// into the path (client.go:97) without url.PathEscape, while the server
	// route is /api/config/set/:namespace/:option/:value (api/config.go:16).
	// A "/" value therefore adds a path segment, and "?"/"#" are parsed as query
	// and fragment, so the value the server stores is not the value passed in.
	cases := []struct {
		name     string
		value    string
		wantPath string // what the client actually puts on the wire
		wantNote string
	}{
		{name: "plain", value: "plain", wantPath: "/api/config/set/ns/k/plain"},
		{name: "slash_adds_segment", value: "a/b", wantPath: "/api/config/set/ns/k/a/b",
			wantNote: "no route matches four segments after /set, so the server 404s"},
		{name: "question_marks_query", value: "a?b", wantPath: "/api/config/set/ns/k/a",
			wantNote: "the value is truncated at the query separator"},
		{name: "hash_marks_fragment", value: "a#b", wantPath: "/api/config/set/ns/k/a",
			wantNote: "the value is truncated at the fragment separator"},
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
			_ = st.req.URL.RequestURI()
		})
	}

	t.Run("slash_value_reaches_server_as_extra_segment", func(t *testing.T) {
		c, st := stubClient(t, `true`, 0)
		if _, err := c.ConfigSet("ns", "k", "a/b"); err != nil {
			t.Fatalf("ConfigSet: %v", err)
		}
		if raw := st.req.URL.RequestURI(); raw != "/api/config/set/ns/k/a/b" {
			t.Errorf("request URI = %q; the value was not escaped", raw)
		}
	})

	t.Run("plugin_category_is_equally_unescaped", func(t *testing.T) {
		c, st := stubClient(t, `[]`, 0)
		if _, err := c.GetPlugins("a/b"); err != nil {
			t.Fatalf("GetPlugins: %v", err)
		}
		if raw := st.req.URL.RequestURI(); raw != "/api/plugins/a/b" {
			t.Errorf("request URI = %q, want the unescaped category", raw)
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
	// Documented defect: no method looks at resp.StatusCode, so any body that
	// happens to decode successfully from an error response is reported to the
	// caller as a success.
	cases := []struct {
		name   string
		status int
		body   string
		call   func(*RemittClient) (any, error)
		want   any
	}{
		{name: "500_with_numeric_body", status: http.StatusInternalServerError, body: `42`,
			call: func(c *RemittClient) (any, error) { return c.PayloadResubmit(1) }, want: int64(42)},
		{name: "403_with_true_body", status: http.StatusForbidden, body: `true`,
			call: func(c *RemittClient) (any, error) { return c.ConfigSet("n", "k", "v") }, want: true},
		{name: "404_with_status_object", status: http.StatusNotFound, body: `{"status":9,"stage":"x"}`,
			call: func(c *RemittClient) (any, error) { return c.GetStatus(1) }, want: JobStatus{Status: 9, Stage: "x"}},
		{name: "401_with_ping_body", status: http.StatusUnauthorized, body: `"PING"`,
			call: func(c *RemittClient) (any, error) { v, _, e := c.Ping(); return v, e }, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := stubClient(t, tc.body, tc.status)
			got, err := tc.call(c)
			if err != nil {
				t.Fatalf("a %d response surfaced as an error: %v", tc.status, err)
			}
			if !reflectDeepEqual(got, tc.want) {
				t.Errorf("value = %#v, want %#v", got, tc.want)
			}
		})
	}

	// The realistic error payloads still fail, but as JSON errors rather than as
	// status-code errors, which is what callers actually see today.
	c, _ := stubClient(t, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
	if _, _, err := c.Ping(); err == nil {
		t.Error("expected the JSON decode error for an echoed error document")
	} else if !strings.Contains(err.Error(), "cannot unmarshal object") {
		t.Errorf("error = %v; want the JSON shape error (the status is never inspected)", err)
	}
}

func TestResponseBodiesAreClosed(t *testing.T) {
	// Documented defect: no method closes resp.Body (client.go:56, 83, 106, 128,
	// 152, 174, 194, 220, 245), so the connection cannot be reused and is leaked
	// until the transport's idle timeout.
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
			if st.respBody.closed {
				t.Errorf("%s closed the response body; if this is fixed, replace the leak note", tc.name)
			} else {
				t.Logf("%s left the response body open (connection leak)", tc.name)
			}
		})
	}
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

func TestUninitialisedClientPanics(t *testing.T) {
	// Documented defect: RemittClient and its fields are exported, so callers can
	// build one without NewClient. The unexported http.Client is then nil and
	// every method panics with a nil pointer dereference instead of returning an
	// error. The panic is pinned here as current behaviour.
	c := &RemittClient{Username: "bob", Password: "s3cr3t", URL: "http://example.invalid"}
	if c.client != nil {
		t.Fatal("zero-value client unexpectedly has an http.Client")
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected a nil-pointer panic from a struct-literal RemittClient")
			return
		}
		if !strings.Contains(strings.ToLower(errString(r)), "nil pointer") {
			t.Errorf("panic = %v, want a nil pointer dereference", r)
		}
	}()
	_, _, _ = c.Ping()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type failingBody struct{ err error }

func (b failingBody) Read([]byte) (int, error) { return 0, b.err }
func (b failingBody) Close() error             { return nil }

func errString(r any) string {
	if e, ok := r.(error); ok {
		return e.Error()
	}
	return ""
}

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
