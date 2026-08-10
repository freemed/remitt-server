package eligibility

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/freemed/remitt-server/model"
)

// Plugin identifiers for BCBSFhirEligibility.
const (
	BCBSFhirEligibilityClass   = "org.remitt.plugin.eligibility.BCBSFhirEligibility"
	BCBSFhirEligibilityVersion = "0.1"
	BCBSFhirEligibilityConfigNS = "eligibility_bcbs_fhir"
)

// Config keys for BCBSFhirEligibility.
var bcbsFhirConfigKeys = []string{
	"bcbsFhirBaseUrl",
	"bcbsFhirClientId",
	"bcbsFhirClientSecret",
	"bcbsFhirInsurerRef",
	"bcbsFhirProviderRef",
}

// Token cache for OAuth2 client credentials.
var (
	bcbsTokenMu      sync.RWMutex
	bcbsTokenValue   string
	bcbsTokenExpires time.Time
)

func init() {
	RegisterChecker(BCBSFhirEligibilityClass, func() EligibilityChecker {
		return &BCBSFhirEligibility{}
	})
}

// BCBSFhirEligibility implements the EligibilityChecker interface for
// BCBS Tennessee via 1upHealth's FHIR R4 REST API.
//
// Flow: values → buildFhirRequest → OAuth2 token → POST CoverageEligibilityRequest
// → parseFhirResponse → EligibilityResponse.
type BCBSFhirEligibility struct {
	ctx context.Context
}

// buildFhirRequest constructs a FHIR R4 CoverageEligibilityRequest JSON
// payload from the given key-value pairs.
func (b *BCBSFhirEligibility) buildFhirRequest(values map[string]string) ([]byte, error) {
	patientID := values["patientId"]
	if patientID == "" {
		patientID = values["patientid"]
	}

	insurerRef := values["insurerRef"]
	if insurerRef == "" {
		insurerRef = values["insurerref"]
	}
	if insurerRef == "" {
		insurerRef = "Organization/bcbs-default"
	}

	providerRef := values["providerRef"]
	if providerRef == "" {
		providerRef = values["providerref"]
	}
	if providerRef == "" {
		providerRef = values["providerId"]
		if providerRef == "" {
			providerRef = values["providerid"]
		}
	}

	benefitCode := values["benefitCode"]
	if benefitCode == "" {
		benefitCode = values["benefitcode"]
	}
	if benefitCode == "" {
		benefitCode = "30"
	}

	req := map[string]interface{}{
		"resourceType": "CoverageEligibilityRequest",
		"id":           fmt.Sprintf("bcbs-fhir-%d", time.Now().UnixNano()),
		"status":       "active",
		"purpose":      []string{"benefits"},
		"patient": map[string]interface{}{
			"reference": "Patient/" + patientID,
		},
		"created": time.Now().UTC().Format(time.RFC3339),
		"insurer": map[string]interface{}{
			"reference": insurerRef,
		},
		"item": []interface{}{
			map[string]interface{}{
				"category": map[string]interface{}{
					"coding": []interface{}{
						map[string]interface{}{
							"system": "http://terminology.hl7.org/CodeSystem/ex-benefitcategory",
							"code":   benefitCode,
						},
					},
				},
			},
		},
	}

	if providerRef != "" {
		req["provider"] = map[string]interface{}{
			"reference": providerRef,
		}
	}

	return json.Marshal(req)
}

// fhirResponse maps the relevant fields from a FHIR CoverageEligibilityResponse.
type fhirResponse struct {
	ResourceType string `json:"resourceType"`
	Status       string `json:"status"`
	Outcome      string `json:"outcome"`
	Disposition  string `json:"disposition"`
	Insurance    []struct {
		Inforce bool `json:"inforce"`
	} `json:"insurance"`
}

// parseFhirResponse parses a FHIR CoverageEligibilityResponse JSON payload
// and maps it to an EligibilityResponse.
func (b *BCBSFhirEligibility) parseFhirResponse(data []byte) (*EligibilityResponse, error) {
	var fhir fhirResponse
	if err := json.Unmarshal(data, &fhir); err != nil {
		return nil, fmt.Errorf("bcbs_fhir: parse response: %w", err)
	}

	resp := &EligibilityResponse{
		Status: StatusOK,
	}

	// Map outcome: "complete" → SUCCESS, anything else → VALIDATION_FAILURE.
	if fhir.Outcome == "complete" {
		// Check insurance.inforce.
		if len(fhir.Insurance) > 0 && fhir.Insurance[0].Inforce {
			resp.SuccessCode = SuccessCodeSuccess
		} else {
			resp.SuccessCode = SuccessCodeValidationFailure
		}
	} else {
		resp.SuccessCode = SuccessCodeValidationFailure
	}

	// Use disposition as the message.
	if fhir.Disposition != "" {
		resp.Messages = []string{fhir.Disposition}
	} else {
		resp.Messages = []string{"FHIR response received"}
	}

	return resp, nil
}

// validateConfig checks that all required config keys are present.
func (b *BCBSFhirEligibility) validateConfig(config map[string]string) error {
	required := []string{"bcbsFhirBaseUrl", "bcbsFhirClientId", "bcbsFhirClientSecret"}
	for _, key := range required {
		if config[key] == "" {
			return fmt.Errorf("bcbs_fhir: required config key %q is missing or empty", key)
		}
	}
	return nil
}

// getOAuthToken returns a cached or freshly-requested OAuth2 token using
// client credentials flow.
func (b *BCBSFhirEligibility) getOAuthToken(baseURL, clientID, clientSecret string) (string, error) {
	bcbsTokenMu.RLock()
	if bcbsTokenValue != "" && time.Now().Before(bcbsTokenExpires) {
		tok := bcbsTokenValue
		bcbsTokenMu.RUnlock()
		return tok, nil
	}
	bcbsTokenMu.RUnlock()

	bcbsTokenMu.Lock()
	defer bcbsTokenMu.Unlock()

	// Double-check after acquiring write lock.
	if bcbsTokenValue != "" && time.Now().Before(bcbsTokenExpires) {
		return bcbsTokenValue, nil
	}

	// Request a new token via OAuth2 client credentials.
	tokenURL := baseURL + "/auth/token"
	reqBody := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s",
		clientID, clientSecret)

	req, err := http.NewRequestWithContext(b.ctx, http.MethodPost, tokenURL,
		bytes.NewBufferString(reqBody))
	if err != nil {
		return "", fmt.Errorf("bcbs_fhir: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bcbs_fhir: token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("bcbs_fhir: token request returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("bcbs_fhir: decode token response: %w", err)
	}

	bcbsTokenValue = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		bcbsTokenExpires = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	} else {
		bcbsTokenExpires = time.Now().Add(1 * time.Hour)
	}

	return bcbsTokenValue, nil
}

// doFhirRequest sends a CoverageEligibilityRequest POST to the FHIR endpoint
// and returns the raw response body.
func (b *BCBSFhirEligibility) doFhirRequest(baseURL, token string, requestBody []byte) ([]byte, error) {
	url := baseURL + "/CoverageEligibilityRequest"

	req, err := http.NewRequestWithContext(b.ctx, http.MethodPost, url,
		bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("bcbs_fhir: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Accept", "application/fhir+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bcbs_fhir: fhir request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bcbs_fhir: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bcbs_fhir: fhir request returned %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// CheckEligibility runs the BCBS FHIR eligibility check.
func (b *BCBSFhirEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	// Load configuration from tUserConfig.
	configValues, err := model.GetConfigValues(userName)
	if err != nil {
		return nil, fmt.Errorf("bcbs_fhir: load config: %w", err)
	}

	config := make(map[string]string)
	for _, cv := range configValues {
		if cv.Namespace == BCBSFhirEligibilityConfigNS {
			config[cv.Option] = cv.Value
		}
	}

	// Validate required configuration.
	if err := b.validateConfig(config); err != nil {
		return nil, err
	}

	baseURL := config["bcbsFhirBaseUrl"]
	clientID := config["bcbsFhirClientId"]
	clientSecret := config["bcbsFhirClientSecret"]

	// Authenticate via OAuth2 client credentials.
	token, err := b.getOAuthToken(baseURL, clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("bcbs_fhir: auth: %w", err)
	}

	// Build FHIR CoverageEligibilityRequest.
	reqBody, err := b.buildFhirRequest(values)
	if err != nil {
		return nil, fmt.Errorf("bcbs_fhir: build request: %w", err)
	}

	// POST to FHIR endpoint.
	respBody, err := b.doFhirRequest(baseURL, token, reqBody)
	if err != nil {
		return nil, fmt.Errorf("bcbs_fhir: fhir call: %w", err)
	}

	// Parse the FHIR CoverageEligibilityResponse.
	return b.parseFhirResponse(respBody)
}

// GetPluginName returns the Java-style dotted class name of this plugin.
func (b *BCBSFhirEligibility) GetPluginName() string {
	return BCBSFhirEligibilityClass
}

// GetPluginVersion returns the plugin version.
func (b *BCBSFhirEligibility) GetPluginVersion() string {
	return BCBSFhirEligibilityVersion
}

// GetPluginConfigurationOptions returns the names of user-configurable
// options required by this plugin.
func (b *BCBSFhirEligibility) GetPluginConfigurationOptions() []string {
	return bcbsFhirConfigKeys
}

// SetContext stores the execution context for use by this plugin.
func (b *BCBSFhirEligibility) SetContext(ctx context.Context) error {
	b.ctx = ctx
	return nil
}
