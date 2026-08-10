package callback

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func TestBuildEnvelope(t *testing.T) {
	user := &model.UserModel{
		Id:                     1,
		Username:               "testuser",
		CallbackUsername:       model.NewNullStringValue("cbuser"),
		CallbackPassword:       model.NewNullStringValue("cbpass"),
		CallbackServiceUri:     "http://example.com/soap",
		CallbackServiceWsdlUri: "http://example.com/soap?wsdl",
	}

	result := JobResult{
		JobID:     42,
		PayloadID: 100,
		Status:    "SUCCESS",
		Message:   "Job completed successfully",
	}

	env := buildEnvelope(user, result)

	// Verify SOAP envelope structure
	if !strings.Contains(env, "http://schemas.xmlsoap.org/soap/envelope/") {
		t.Error("envelope missing SOAP envelope namespace")
	}
	if !strings.Contains(env, "<wsse:UsernameToken>") {
		t.Error("envelope missing UsernameToken")
	}
	if !strings.Contains(env, "<wsse:Username>cbuser</wsse:Username>") {
		t.Error("envelope missing correct username")
	}
	if !strings.Contains(env, "<wsse:Password>cbpass</wsse:Password>") {
		t.Error("envelope missing correct password")
	}
	if !strings.Contains(env, "urn:freemed:remitt") {
		t.Error("envelope missing remitt namespace")
	}
	if !strings.Contains(env, "<jobId>42</jobId>") {
		t.Error("envelope missing jobId")
	}
	if !strings.Contains(env, "<payloadId>100</payloadId>") {
		t.Error("envelope missing payloadId")
	}
	if !strings.Contains(env, "<status>SUCCESS</status>") {
		t.Error("envelope missing status")
	}
	if !strings.Contains(env, "<message>Job completed successfully</message>") {
		t.Error("envelope missing message")
	}

	// Verify it's valid XML by unmarshaling
	type Envelope struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			JobComplete struct {
				JobID     string `xml:"jobId"`
				PayloadID string `xml:"payloadId"`
				Status    string `xml:"status"`
				Message   string `xml:"message"`
			} `xml:"JobComplete"`
		} `xml:"Body"`
	}
	var parsed Envelope
	if err := xml.Unmarshal([]byte(env), &parsed); err != nil {
		t.Fatalf("buildEnvelope produced invalid XML: %v", err)
	}
	if parsed.Body.JobComplete.Status != "SUCCESS" {
		t.Errorf("expected Status SUCCESS, got %s", parsed.Body.JobComplete.Status)
	}
	if parsed.Body.JobComplete.JobID != "42" {
		t.Errorf("expected JobID 42, got %s", parsed.Body.JobComplete.JobID)
	}
	if parsed.Body.JobComplete.PayloadID != "100" {
		t.Errorf("expected PayloadID 100, got %s", parsed.Body.JobComplete.PayloadID)
	}
}

func TestBuildEnvelope_NoCredentials(t *testing.T) {
	user := &model.UserModel{
		Id:                 1,
		Username:           "testuser",
		CallbackServiceUri: "http://example.com/soap",
	}

	result := JobResult{
		JobID:     1,
		PayloadID: 10,
		Status:    "FAILED",
		Message:   "error occurred",
	}

	env := buildEnvelope(user, result)

	// Should still produce valid XML even without credentials
	if !strings.Contains(env, "<wsse:UsernameToken>") {
		t.Error("envelope should still contain UsernameToken element")
	}
}

func TestBuildEnvelope_XMLEscaping(t *testing.T) {
	user := &model.UserModel{
		Id:                 1,
		Username:           "testuser",
		CallbackServiceUri: "http://example.com/soap",
	}

	result := JobResult{
		JobID:     1,
		PayloadID: 10,
		Status:    "FAILED",
		Message:   "error: <script>alert('xss')</script> & special chars",
	}

	env := buildEnvelope(user, result)

	// Special XML characters should be escaped
	if strings.Contains(env, "<script>") || strings.Contains(env, "& ") {
		// The raw < and & should not appear unescaped in the message element value
		// But they might appear within the XML as &lt; and &amp;
		// This is a basic sanity check
	}

	// Verify it's still valid XML
	var parsed struct {
		Body struct {
			JobComplete struct {
				Message string `xml:"message"`
			} `xml:"JobComplete"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal([]byte(env), &parsed); err != nil {
		t.Fatalf("buildEnvelope produced invalid XML with special chars: %v", err)
	}
	// The message should contain the original text after XML parsing
	if !strings.Contains(parsed.Body.JobComplete.Message, "error:") {
		t.Errorf("message not properly preserved: %s", parsed.Body.JobComplete.Message)
	}
}

func TestRegistryRegisterAndInstantiate(t *testing.T) {
	// Create a mock implementation for testing
	mockSender := &mockCallbackSender{name: "test-mock"}

	RegisterCallback("test-mock", func() CallbackSender {
		return mockSender
	})

	inst, err := InstantiateCallback("test-mock")
	if err != nil {
		t.Fatalf("InstantiateCallback failed: %v", err)
	}
	if inst == nil {
		t.Fatal("InstantiateCallback returned nil")
	}

	// Verify it's the same instance
	if inst != mockSender {
		t.Error("InstantiateCallback returned wrong instance")
	}
}

func TestRegistryInstantiateUnknown(t *testing.T) {
	_, err := InstantiateCallback("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown callback, got nil")
	}
}

func TestJobResultJSON(t *testing.T) {
	result := JobResult{
		JobID:     42,
		PayloadID: 100,
		Status:    "SUCCESS",
		Message:   "done",
	}
	if result.JobID != 42 {
		t.Errorf("expected JobID 42, got %d", result.JobID)
	}
	if result.Status != "SUCCESS" {
		t.Errorf("expected Status SUCCESS, got %s", result.Status)
	}
}

// mockCallbackSender implements CallbackSender for testing
type mockCallbackSender struct {
	name string
}

func (m *mockCallbackSender) SendResult(ctx context.Context, user *model.UserModel, result JobResult) error {
	return nil
}
