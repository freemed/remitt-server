package callback

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"strconv"
	"text/template"
	"time"

	"github.com/freemed/remitt-server/model"
)

const (
	defaultTimeout = 30 * time.Second

	soapEnvelopeTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Header>
    <wsse:Security xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">
      <wsse:UsernameToken>
        <wsse:Username>{{.Username | xmlEscape}}</wsse:Username>
        <wsse:Password>{{.Password | xmlEscape}}</wsse:Password>
      </wsse:UsernameToken>
    </wsse:Security>
  </soap:Header>
  <soap:Body>
    <remitt:JobComplete xmlns:remitt="urn:freemed:remitt">
      <jobId>{{.JobID}}</jobId>
      <payloadId>{{.PayloadID}}</payloadId>
      <status>{{.Status | xmlEscape}}</status>
      <message>{{.Message | xmlEscape}}</message>
    </remitt:JobComplete>
  </soap:Body>
</soap:Envelope>`
)

// envelopeData holds the data for the SOAP envelope template.
type envelopeData struct {
	Username  string
	Password  string
	JobID     string
	PayloadID string
	Status    string
	Message   string
}

// SoapCallback implements CallbackSender using SOAP over HTTP.
type SoapCallback struct {
	client  *http.Client
	timeout time.Duration
}

// NewSoapCallback creates a new SoapCallback with the default timeout.
func NewSoapCallback() *SoapCallback {
	return &SoapCallback{
		client: &http.Client{
			Timeout: defaultTimeout,
		},
		timeout: defaultTimeout,
	}
}

// NewSoapCallbackWithTimeout creates a new SoapCallback with a custom timeout.
func NewSoapCallbackWithTimeout(timeout time.Duration) *SoapCallback {
	return &SoapCallback{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// SendResult sends a job completion notification via SOAP to the originating system.
func (s *SoapCallback) SendResult(ctx context.Context, user *model.UserModel, result JobResult) error {
	if user.CallbackServiceUri == "" {
		return nil // No callback URI configured; nothing to do
	}

	envelope := buildEnvelope(user, result)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, user.CallbackServiceUri, bytes.NewReader([]byte(envelope)))
	if err != nil {
		return fmt.Errorf("soap callback: create request: %w", err)
	}

	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", "urn:freemed:remitt/JobComplete")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("soap callback: send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("soap callback: server returned %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("callback.SoapCallback: Job %d (%s) notified to %s (HTTP %d)",
		result.JobID, result.Status, user.CallbackServiceUri, resp.StatusCode)
	_ = body
	return nil
}

// buildEnvelope constructs the SOAP XML envelope for the callback notification.
func buildEnvelope(user *model.UserModel, result JobResult) string {
	data := envelopeData{
		Username:  user.CallbackUsername.String,
		Password:  user.CallbackPassword.String,
		JobID:     strconv.FormatInt(result.JobID, 10),
		PayloadID: strconv.FormatInt(result.PayloadID, 10),
		Status:    result.Status,
		Message:   result.Message,
	}

	var buf bytes.Buffer
	tmpl, err := template.New("soap").Funcs(template.FuncMap{
		"xmlEscape": html.EscapeString,
	}).Parse(soapEnvelopeTemplate)
	if err != nil {
		log.Printf("callback.buildEnvelope: template parse error: %v", err)
		return ""
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("callback.buildEnvelope: template execute error: %v", err)
		return ""
	}
	return buf.String()
}

func init() {
	RegisterCallback("soap", func() CallbackSender {
		return NewSoapCallback()
	})
}
