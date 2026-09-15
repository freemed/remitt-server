package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/freemed/remitt-server/model"
)

// ErrUninitialisedClient is returned by every method when the RemittClient was
// built as a struct literal instead of with NewClient. The unexported
// *http.Client is nil in that case, so no request could be attempted; an error
// is returned instead of dereferencing the nil client.
var ErrUninitialisedClient = errors.New("client: uninitialised RemittClient, use NewClient")

// RemittClient is a REMITT API client interface.
type RemittClient struct {
	Username string
	Password string
	URL      string
	client   *http.Client
}

// NewClient instantiates a new RemittClient instance
func NewClient(username, password, url string) (*RemittClient, error) {
	cl := &RemittClient{
		Username: username,
		Password: password,
		URL:      url,
	}
	err := cl.init()
	return cl, err
}

// init performs internal initialization of the client
func (c *RemittClient) init() error {
	client := &http.Client{
		Timeout: time.Second * time.Duration(30),
	}
	c.client = client
	return nil
}

// do performs req with the client's HTTP client. It reports an error for a
// client that was never initialised (a struct literal has a nil http.Client)
// and for any response that does not carry a 2xx status, so a body that merely
// happens to decode is never mistaken for a successful call. On error the
// response body is drained and closed before returning, so the connection goes
// back to the pool.
func (c *RemittClient) do(req *http.Request) (*http.Response, error) {
	if c == nil || c.client == nil {
		return nil, ErrUninitialisedClient
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("client: unexpected HTTP status %d %s for %s %s",
			resp.StatusCode, http.StatusText(resp.StatusCode), req.Method, req.URL.Redacted())
	}
	return resp, nil
}

// Ping is a simple command to determine if the API is functional
func (c *RemittClient) Ping() (bool, time.Duration, error) {
	var pingText = "PING"
	startTime := time.Now()
	// The configured base URL is data, not a format string: passing it to
	// fmt.Sprintf as the format rewrote any "%" in it into printf error
	// markers, which failed url.Parse before a request was ever attempted.
	req, err := http.NewRequest(http.MethodGet, c.URL+"/api/ping/"+pingText, nil)
	if err != nil {
		return false, time.Since(startTime), err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return false, time.Since(startTime), err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, time.Since(startTime), err
	}
	var out string
	err = json.Unmarshal(body, &out)
	if err != nil {
		return false, time.Since(startTime), err
	}
	if out != pingText {
		return false, time.Since(startTime), fmt.Errorf("%s != %s", out, pingText)
	}
	return true, time.Since(startTime), nil
}

// ConfigGetAll retrieves all user configurable variables
func (c *RemittClient) ConfigGetAll() ([]model.UserConfigModel, error) {
	var out []model.UserConfigModel
	req, err := http.NewRequest(http.MethodGet, c.URL+"/api/config/all", nil)
	if err != nil {
		return out, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return out, err
	}
	return out, nil
}

// ConfigSet sets a value for a user configurable variable
func (c *RemittClient) ConfigSet(namespace, key, value string) (bool, error) {
	var out bool
	// The route is /api/config/set/:namespace/:option/:value, so each argument
	// is a single path segment and must be escaped: an unescaped "/" adds a
	// segment (no route matches) while "?" and "#" truncate the value the
	// server actually stores.
	path := "/api/config/set/" +
		url.PathEscape(namespace) + "/" +
		url.PathEscape(key) + "/" +
		url.PathEscape(value)
	req, err := http.NewRequest(http.MethodPost, c.URL+path, nil)
	if err != nil {
		return out, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return out, err
	}
	return out, nil
}

// CurrentUser retrieves the current user name
func (c *RemittClient) CurrentUser() (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.URL+"/api/currentuser", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var out string
	err = json.Unmarshal(body, &out)
	if err != nil {
		return "", err
	}
	return out, nil
}

// GetStatus retrieves the specified job status
func (c *RemittClient) GetStatus(id int64) (JobStatus, error) {
	var out JobStatus
	req, err := http.NewRequest(http.MethodGet, c.URL+fmt.Sprintf("/api/status/%d", id), nil)
	if err != nil {
		return JobStatus{}, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return JobStatus{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return JobStatus{}, err
	}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return JobStatus{}, err
	}
	return out, nil
}

// GetPlugins retrieves a list of plugins for a specific category
func (c *RemittClient) GetPlugins(category string) ([]string, error) {
	// The route is /api/plugins/get/:category, so the category is a single path
	// segment and has to be escaped like the ConfigSet arguments.
	req, err := http.NewRequest(http.MethodGet, c.URL+"/api/plugins/"+url.PathEscape(category), nil)
	if err != nil {
		return []string{}, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return []string{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []string{}, err
	}
	var out []string
	err = json.Unmarshal(body, &out)
	if err != nil {
		return []string{}, err
	}
	return out, nil
}

// PayloadInsert inserts a new payload of data for processing
func (c *RemittClient) PayloadInsert(payload InputPayload) (int64, error) {
	payloadBody, err := c.objToReaderJSON(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, c.URL+"/api/payload/", payloadBody)
	if err != nil {
		return 0, err
	}
	// api.PayloadInsert binds the body with echo's DefaultBinder
	// (api/payload.go:37), which answers 415 for a request with no
	// Content-Type, so the JSON document this method sends must be announced as
	// application/json. No other method sends a body: ConfigSet POSTs with an
	// empty one and its handler (api/config.go:31) only reads path parameters,
	// and every other call is a GET.
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var out int64
	err = json.Unmarshal(body, &out)
	if err != nil {
		return 0, err
	}
	return out, nil
}

// PayloadResubmit resubmits an already existing REMITT payload for processing
func (c *RemittClient) PayloadResubmit(id int64) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, c.URL+fmt.Sprintf("/api/payload/resubmit/%d", id), nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var out int64
	err = json.Unmarshal(body, &out)
	if err != nil {
		return 0, err
	}
	return out, nil
}

// ProtocolVersion retrieves the current version of the REMITT protocol
func (c *RemittClient) ProtocolVersion() (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.URL+"/api/version/protocol", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var out string
	err = json.Unmarshal(body, &out)
	if err != nil {
		return "", err
	}
	return out, nil
}

// objToReaderJSON marshals obj as JSON and hands it back as a reader. The
// json.Marshal error is propagated: discarding it sent an unencodable value to
// the server as a silent zero-length body.
func (c *RemittClient) objToReaderJSON(obj any) (io.Reader, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return bytes.NewBuffer(b), nil
}
