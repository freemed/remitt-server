package eligibility

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/freemed/remitt-server/model"
)

const (
	// OptumPluginName is the Java-style dotted class name used in the registry
	// and plugin configuration.
	OptumPluginName = "org.remitt.plugin.eligibility.OptumEligibility"

	// OptumPluginVersion tracks the plugin version.
	OptumPluginVersion = "0.1"

	// OptumConfigNS is the tUserConfig namespace for Optum eligibility settings.
	OptumConfigNS = "eligibility_optum"

	// Default Optum sandbox / production base URL.
	optumDefaultBaseURL = "https://apigw.changehealthcare.com"
)

// Optum config keys exposed via GetPluginConfigurationOptions and read from
// tUserConfig.
var optumConfigKeys = []string{
	"optumClientId",
	"optumClientSecret",
	"optumBaseUrl",
	"optumTradingPartnerId",
	"optumProviderNpi",
	"optumProviderTaxId",
}

// tokenCache holds a cached OAuth2 access token with its expiry time.
// All fields are protected by mu for concurrent access.
type tokenCache struct {
	token     string
	expiresAt time.Time
	mu        sync.RWMutex
}

// get returns the cached token if it is still valid, otherwise an empty string.
func (tc *tokenCache) get() string {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	if time.Now().Before(tc.expiresAt) {
		return tc.token
	}
	return ""
}

// set stores a new token and its expiry time.
func (tc *tokenCache) set(token string, expiresAt time.Time) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.token = token
	tc.expiresAt = expiresAt
}

// optumToken is the package-level token cache shared across all
// OptumEligibility instances.
var optumToken = &tokenCache{}

func init() {
	RegisterChecker(OptumPluginName, func() EligibilityChecker {
		return &OptumEligibility{}
	})
}

// OptumEligibility checks patient eligibility via the Optum (Change Healthcare)
// REST API. It authenticates with OAuth2 client credentials, builds a JSON
// eligibility request matching the Optum /medicalnetwork/eligibility/v3
// schema, and parses the response into an EligibilityResponse.
type OptumEligibility struct {
	clientId          string
	clientSecret      string
	baseURL           string
	tradingPartnerId  string
	providerNpi       string
	providerTaxId     string
	configured        bool
	ctx               context.Context
}

// --- EligibilityChecker interface ---

func (o *OptumEligibility) GetPluginName() string {
	return OptumPluginName
}

func (o *OptumEligibility) GetPluginVersion() string {
	return OptumPluginVersion
}

func (o *OptumEligibility) GetPluginConfigurationOptions() []string {
	return optumConfigKeys
}

func (o *OptumEligibility) SetContext(ctx context.Context) error {
	o.ctx = ctx
	return nil
}

// CheckEligibility authenticates against the Optum OAuth2 endpoint, builds a
// JSON eligibility request, POSTs it to the /medicalnetwork/eligibility/v3
// endpoint, and returns the result as an EligibilityResponse.
//
// Configuration is loaded lazily from tUserConfig on the first call.
func (o *OptumEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	if !o.configured {
		if err := o.loadConfig(userName); err != nil {
			return nil, fmt.Errorf("optum: config: %w", err)
		}
	}

	// 1. Obtain a Bearer token (from cache or OAuth2).
	token, err := o.getToken()
	if err != nil {
		return nil, fmt.Errorf("optum: auth: %w", err)
	}

	// 2. Build the JSON request body.
	reqBody := o.buildRequestBody(values)
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("optum: marshal request: %w", err)
	}

	// 3. POST to the eligibility endpoint.
	eligURL := o.baseURL + "/medicalnetwork/eligibility/v3"
	httpReq, err := http.NewRequestWithContext(o.ctx, http.MethodPost, eligURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("optum: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("optum: http post: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB limit
	if err != nil {
		return nil, fmt.Errorf("optum: read response: %w", err)
	}

	// 4. Parse response into EligibilityResponse.
	return o.parseResponse(resp.StatusCode, respBytes)
}

// --- Config ---

// loadConfig reads Optum configuration from tUserConfig (namespace "eligibility_optum").
func (o *OptumEligibility) loadConfig(userName string) error {
	if model.Queries == nil {
		return fmt.Errorf("load config: database not initialized")
	}
	configValues, err := model.GetConfigValues(userName)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	params := make(map[string]string)
	for _, cv := range configValues {
		if cv.Namespace == OptumConfigNS {
			params[cv.Option] = cv.Value
		}
	}

	o.clientId = params["optumClientId"]
	o.clientSecret = params["optumClientSecret"]
	o.baseURL = params["optumBaseUrl"]
	o.tradingPartnerId = params["optumTradingPartnerId"]
	o.providerNpi = params["optumProviderNpi"]
	o.providerTaxId = params["optumProviderTaxId"]

	if o.baseURL == "" {
		o.baseURL = optumDefaultBaseURL
	}

	// Required keys.
	if o.clientId == "" {
		return fmt.Errorf("optumClientId not configured")
	}
	if o.clientSecret == "" {
		return fmt.Errorf("optumClientSecret not configured")
	}
	if o.tradingPartnerId == "" {
		return fmt.Errorf("optumTradingPartnerId not configured")
	}
	if o.providerNpi == "" {
		return fmt.Errorf("optumProviderNpi not configured")
	}

	o.configured = true
	return nil
}

// --- OAuth2 ---

// getToken returns a valid OAuth2 Bearer token, reusing a cached token when
// possible or fetching a new one from the Optum token endpoint.
func (o *OptumEligibility) getToken() (string, error) {
	if tok := optumToken.get(); tok != "" {
		return tok, nil
	}
	return o.fetchToken()
}

// fetchToken calls the Optum OAuth2 token endpoint with client_credentials
// grant and caches the result.
func (o *OptumEligibility) fetchToken() (string, error) {
	tokenURL := o.baseURL + "/oauth2/token"
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {o.clientId},
		"client_secret": {o.clientSecret},
	}

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<18)) // 256 KiB limit
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(respBytes, &tokenResp); err != nil {
		return "", fmt.Errorf("unmarshal token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token endpoint returned empty access_token")
	}

	// Cache the token, expiring slightly early (30s buffer) to avoid
	// edge cases where the token expires mid-request.
	ttl := time.Duration(tokenResp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 3600 * time.Second // default 1h
	}
	expiresAt := time.Now().Add(ttl - 30*time.Second)
	optumToken.set(tokenResp.AccessToken, expiresAt)

	return tokenResp.AccessToken, nil
}

// --- Request / Response ---

// optumEligibilityRequest mirrors the Optum /medicalnetwork/eligibility/v3
// JSON schema.
type optumEligibilityRequest struct {
	TradingPartnerServiceId string              `json:"tradingPartnerServiceId"`
	Provider                optumProvider       `json:"provider"`
	Subscriber              optumSubscriber     `json:"subscriber"`
	ServiceTypeCodes        []string            `json:"serviceTypeCodes,omitempty"`
}

type optumProvider struct {
	NPI              string `json:"npi"`
	OrganizationName string `json:"organizationName,omitempty"`
	TaxID            string `json:"taxId,omitempty"`
}

type optumSubscriber struct {
	MemberID    string `json:"memberId"`
	FirstName   string `json:"firstName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	DateOfBirth string `json:"dateOfBirth,omitempty"`
}

// buildRequestBody constructs the Optum JSON request from the configured
// plugin settings and the values map provided by the caller.
func (o *OptumEligibility) buildRequestBody(values map[string]string) optumEligibilityRequest {
	req := optumEligibilityRequest{
		TradingPartnerServiceId: o.tradingPartnerId,
		Provider: optumProvider{
			NPI:  o.providerNpi,
			TaxID: o.providerTaxId,
		},
		Subscriber: optumSubscriber{
			MemberID:    values["memberId"],
			FirstName:   values["firstName"],
			LastName:    values["lastName"],
			DateOfBirth: values["dateOfBirth"],
		},
	}

	if svcCodes, ok := values["serviceTypeCodes"]; ok && svcCodes != "" {
		req.ServiceTypeCodes = []string{svcCodes}
	} else if svcCodes, ok := values["serviceTypes"]; ok && svcCodes != "" {
		req.ServiceTypeCodes = []string{svcCodes}
	} else {
		// Default: health benefit plan coverage (30).
		req.ServiceTypeCodes = []string{"30"}
	}

	if orgName := values["organizationName"]; orgName != "" {
		req.Provider.OrganizationName = orgName
	}

	return req
}

// parseResponse converts an Optum eligibility API HTTP response into an
// EligibilityResponse.
func (o *OptumEligibility) parseResponse(statusCode int, body []byte) (*EligibilityResponse, error) {
	r := &EligibilityResponse{
		Status: StatusOK,
	}

	if statusCode >= 200 && statusCode < 300 {
		r.SuccessCode = SuccessCodeSuccess
		r.Messages = []string{"Optum eligibility check completed"}
		// Best-effort extract any status/messages from the JSON.
		var raw map[string]interface{}
		if json.Unmarshal(body, &raw) == nil {
			if msgs, ok := raw["messages"].([]interface{}); ok {
				r.Messages = make([]string, 0, len(msgs))
				for _, m := range msgs {
					if s, ok := m.(string); ok {
						r.Messages = append(r.Messages, s)
					}
				}
			}
		}
	} else {
		r.SuccessCode = SuccessCodeValidationFailure
		r.Messages = []string{fmt.Sprintf("Optum API returned status %d: %s", statusCode, string(body))}
	}

	return r, nil
}
