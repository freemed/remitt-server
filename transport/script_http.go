package transport

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
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

// ---------------------------------------------------------------------------
// GoQuery, script-reachable
// ---------------------------------------------------------------------------

// GoQuery hands a *goquery.Document back to Go callers; it cannot serve a
// plugin script, because otto cannot convert a JavaScript string into the
// []byte it takes ("TypeError: can't convert from \"string\" to \"[]uint8\"")
// nor hand a *goquery.Document back to JavaScript. The two helpers below are
// the script-reachable form of the same capability: they take the document as
// a string and give back strings, which is all a DOM object could ever be
// reduced to across the otto boundary. A script therefore scrapes with
//
//	page = http.Get(url);
//	status = http.GoQueryText(page, "span.status");
//	link = http.GoQueryAttribute(page, "a.claim", "href");
//
// The Go query calls themselves are unchanged, so Go callers see the same
// surface as before.

// queryFail renders the string a script receives when the document or the
// selector it supplied cannot be used. It carries the same httpFailurePrefix
// as fail(): a script must never mistake a failure for extracted content
// (http.Get(...).indexOf("HTTP-ERROR: ") === 0).
func (o *httpclient) queryFail(op, selector string, err error) string {
	log.Printf("JS.http.%s: %s: selector %s: %s", op, o.obj.user.Username, selector, err.Error())
	return fmt.Sprintf("%shttp.%s: %s: %s", httpFailurePrefix, op, selector, err)
}

// selectFirst parses body as HTML and resolves selector to its first match.
// The second return value is the failure string to hand the script instead
// (empty on success). Only a document that cannot be parsed fails here: a
// selector cascadia rejects is compiled by goquery into a matcher that fails
// every match (goquery v1.11/v1.12 type.go:compileMatcher -> invalidMatcher),
// so a typo'd selector reads as "matched nothing" - the same value as an
// absent element - and never as a panic unwinding out of otto into the job
// worker.
func (o *httpclient) selectFirst(op, body, selector string) (*goquery.Selection, string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, o.queryFail(op, selector, fmt.Errorf("parsing document: %w", err))
	}
	return doc.Find(selector).First(), ""
}

// GoQueryText returns the text of the first element matching selector, with
// surrounding whitespace trimmed. It returns "" when the selector matches
// nothing or cannot be compiled (see selectFirst), and a failure string, never
// a panic, when the document cannot be parsed at all.
func (o *httpclient) GoQueryText(body string, selector string) string {
	log.Printf("JS.http.GoQueryText: %s %s", o.obj.user.Username, selector)
	sel, failure := o.selectFirst("GoQueryText", body, selector)
	if failure != "" {
		return failure
	}
	return strings.TrimSpace(sel.Text())
}

// GoQueryAttribute returns the value of attribute on the first element
// matching selector, or "" when the element or the attribute is absent, or a
// failure string for the same document failure as GoQueryText.
func (o *httpclient) GoQueryAttribute(body string, selector string, attribute string) string {
	log.Printf("JS.http.GoQueryAttribute: %s %s %s", o.obj.user.Username, selector, attribute)
	sel, failure := o.selectFirst("GoQueryAttribute", body, selector)
	if failure != "" {
		return failure
	}
	value, ok := sel.Attr(attribute)
	if !ok {
		return ""
	}
	return value
}
