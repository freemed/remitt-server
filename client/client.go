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

// ConfigGetAll retrieves all user configurable variables
func (c *RemittClient) ConfigGetAll() ([]model.UserConfigModel, error) {
	resp, err := c.client.Get(c.URL + "/api/config/all")
	if err != nil {
		return []model.UserConfigModel{}, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []model.UserConfigModel{}, err
	}
	var out []model.UserConfigModel
	err = json.Unmarshal(body, &out)
	if err != nil {
		return []model.UserConfigModel{}, err
	}
	return out, nil
}

// ConfigSet sets a value for a user configurable variable
func (c *RemittClient) ConfigSet(namespace, key, value string) (bool, error) {
	resp, err := c.client.Post(c.URL+fmt.Sprintf("/api/config/set/%s/%s/%s", namespace, key, value), "application/json", nil)
	if err != nil {
		return false, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	var out bool
	err = json.Unmarshal(body, &out)
	if err != nil {
		return false, err
	}
	return out, nil
}

// CurrentUser retrieves the current user name
func (c *RemittClient) CurrentUser() (string, error) {
	resp, err := c.client.Get(c.URL + "/api/currentuser")
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
	resp, err := c.client.Get(c.URL + fmt.Sprintf("/api/status/%d", id))
	if err != nil {
		return JobStatus{}, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return JobStatus{}, err
	}
	var out JobStatus
	err = json.Unmarshal(body, &out)
	if err != nil {
		return JobStatus{}, err
	}
	return out, nil
}

// GetPlugins retrieves a list of plugins for a specific category
func (c *RemittClient) GetPlugins(category string) ([]string, error) {
	resp, err := c.client.Get(c.URL + fmt.Sprintf("/api/plugins/%s", category))
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
	resp, err := c.client.Post(c.URL+"/api/payload/", "application/json", c.objToReaderJSON(payload))
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
	resp, err := c.client.Get(c.URL + fmt.Sprintf("/api/payload/resubmit/%d", id))
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

func (c *RemittClient) objToReaderJSON(obj interface{}) io.Reader {
	b, _ := json.Marshal(obj)
	return bytes.NewBuffer(b)
}
