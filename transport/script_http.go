package transport

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// defaultHTTPTimeout bounds a request when HTTPTimeout is not positive. It
// mirrors the 30s bound the other outbound HTTP clients in this repository
// apply (callback.defaultTimeout, eligibility.StediTimeout).
const defaultHTTPTimeout = 30 * time.Second

// HTTPTimeout bounds every request made by the scripted HTTP helpers
// (http.Get / http.GetWithBasicAuth), so a hung payer endpoint cannot hold a
// job worker open forever.
//
// It is the configuration knob for the scripted HTTP transport: config.AppConfig
// carries no HTTP timeout field (the config.Config.Timeouts.HTTPTimeout the
// helpers used to reference in commented-out code does not exist), so process
// wiring sets it here. A non-positive value falls back to defaultHTTPTimeout
// rather than silently disabling the timeout.
var HTTPTimeout = defaultHTTPTimeout

// httpFailurePrefix starts the string a helper hands back when a request did
// not produce a 2xx response. Scripts must treat such a value as a failure
// (http.Get(url).indexOf("HTTP-ERROR: ") === 0): a request that succeeded
// returns the response body verbatim, so an empty string means "the server
// answered 2xx with an empty body" and never a failure.
const httpFailurePrefix = "HTTP-ERROR: "

// httpBodyLogLimit is how much of a non-2xx response body is written to the job
// log for diagnosis. The body itself is never returned to the script.
const httpBodyLogLimit = 512

// newHTTPClient builds the client used by both helpers with the configured
// HTTP timeout applied.
func newHTTPClient() *http.Client {
	timeout := HTTPTimeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return &http.Client{Timeout: timeout}
}

type httpclient struct {
	obj *Interpreter
}

// fail logs a request that did not succeed and renders the failure string the
// script receives in place of a response body.
func (o *httpclient) fail(op, url string, err error) string {
	log.Printf("JS.http.%s: %s %s: The HTTP request failed with error %s", op, o.obj.user.Username, url, err.Error())
	return fmt.Sprintf("%shttp.%s: %s: %s", httpFailurePrefix, op, url, err)
}

// do performs a GET and returns the response body for a 2xx response. Any
// other outcome (unparsable URL, connection failure, timeout, non-2xx status,
// unreadable body) is surfaced to the script as an error string instead of
// being mistaken for a body.
func (o *httpclient) do(op, url, userAgent, username, password string, basicAuth bool) string {
	log.Printf("JS.http.%s: %s %s", op, o.obj.user.Username, url)

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return o.fail(op, url, fmt.Errorf("invalid URL: %w", err))
	}
	request.Header.Set("User-Agent", userAgent)
	if basicAuth {
		request.SetBasicAuth(username, password)
	}

	response, err := newHTTPClient().Do(request)
	if err != nil {
		// Connection refusal, DNS failure, TLS failure, timeout, ...
		return o.fail(op, url, err)
	}
	defer func() {
		if cerr := response.Body.Close(); cerr != nil {
			log.Printf("JS.http.%s: %s %s: closing response body: %s", op, o.obj.user.Username, url, cerr.Error())
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return o.fail(op, url, fmt.Errorf("reading response body: %w", err))
	}

	if response.StatusCode < http.StatusOK || response.StatusCode > 299 {
		// A rejection is a failure, not a successful payload: log what the
		// server said and hand the script an error string.
		log.Printf("JS.http.%s: %s %s: HTTP %s: %s", op, o.obj.user.Username, url, response.Status, bodySnippet(body))
		return o.fail(op, url, fmt.Errorf("HTTP %s", response.Status))
	}

	return string(body)
}

func (o *httpclient) Get(url string) string {
	return o.do("Get", url, "upload-server/2.0", "", "", false)
}

func (o *httpclient) GetWithBasicAuth(url string, username string, password string) string {
	return o.do("GetWithBasicAuth", url, "remitt/0.9", username, password, true)
}

// bodySnippet renders a bounded, log-safe copy of a response body.
func bodySnippet(body []byte) string {
	if len(body) > httpBodyLogLimit {
		return string(body[:httpBodyLogLimit]) + "... (truncated)"
	}
	return string(body)
}

func (o *httpclient) GoQuery(body []byte) *goquery.Document {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		log.Printf("JS.http.GoQuery: %s: The HTTP request failed with error %s", o.obj.user.Username, err.Error())
		return nil
	}
	return doc
}
