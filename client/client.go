package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/freemed/remitt-server/model"
)

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

// Ping is a simple command to determine if the API is functional
func (c *RemittClient) Ping() (bool, time.Duration, error) {
	var pingText = "PING"
	startTime := time.Now()
	req, err := http.NewRequest("GET", fmt.Sprintf(c.URL+"/api/ping/%s", pingText), nil)
	if err != nil {
		return false, time.Since(startTime), err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return false, time.Since(startTime), err
	}
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
	req, err := http.NewRequest("GET", c.URL+"/api/config/all", nil)
	if err != nil {
		return out, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return out, err
	}
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
	req, err := http.NewRequest("POST", c.URL+fmt.Sprintf("/api/config/set/%s/%s/%s", namespace, key, value), nil)
	if err != nil {
		return out, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return out, err
	}
	body, err := ioutil.ReadAll(resp.Body)
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
	req, err := http.NewRequest("GET", c.URL+"/api/currentuser", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	body, err := ioutil.ReadAll(resp.Body)
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
	req, err := http.NewRequest("GET", c.URL+fmt.Sprintf("/api/status/%d", id), nil)
	if err != nil {
		return JobStatus{}, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return JobStatus{}, err
	}
	body, err := ioutil.ReadAll(resp.Body)
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
	req, err := http.NewRequest("GET", c.URL+fmt.Sprintf("/api/plugins/%s", category), nil)
	if err != nil {
		return []string{}, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return []string{}, err
	}
	body, err := ioutil.ReadAll(resp.Body)
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
	req, err := http.NewRequest("POST", c.URL+"/api/payload/", c.objToReaderJSON(payload))
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	body, err := ioutil.ReadAll(resp.Body)
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
	req, err := http.NewRequest("GET", c.URL+fmt.Sprintf("/api/payload/resubmit/%d", id), nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
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
	req, err := http.NewRequest("GET", c.URL+"/api/version/protocol", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
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

func (c *RemittClient) objToReaderJSON(obj any) io.Reader {
	b, _ := json.Marshal(obj)
	return bytes.NewBuffer(b)
}
