package eligibility

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/freemed/remitt-server/model"
)

const (
	StediPluginName    = "org.remitt.plugin.eligibility.StediEligibility"
	StediPluginVersion = "0.1"
	StediConfigNS      = "eligibility_stedi"
	StediAPIEndpoint   = "https://api.stedi.com/change/medicalnetwork/eligibility/v3"
	StediTimeout       = 30 * time.Second
)

var stediConfigKeys = []string{
	"stediApiKey",
	"stediTradingPartnerId",
	"stediProviderNpi",
	"stediProviderTaxId",
}

func init() {
	RegisterChecker(StediPluginName, func() EligibilityChecker {
		return &StediEligibility{}
	})
}

// stediRequest is the JSON payload sent to the Stedi eligibility API.
type stediRequest struct {
	TradingPartnerServiceID string         `json:"tradingPartnerServiceId"`
	Provider                stediProvider  `json:"provider"`
	Subscriber              stediSubscriber `json:"subscriber"`
	ServiceTypeCodes        []string       `json:"serviceTypeCodes"`
}

type stediProvider struct {
	NPI              string `json:"npi"`
	OrganizationName string `json:"organizationName,omitempty"`
}

type stediSubscriber struct {
	MemberID    string `json:"memberId"`
	FirstName   string `json:"firstName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	DateOfBirth string `json:"dateOfBirth,omitempty"`
}

// stediResponse is the JSON response from the Stedi eligibility API.
type stediResponse struct {
	Subscriber stediResponseSubscriber `json:"subscriber"`
}

type stediResponseSubscriber struct {
	EligibilityStatus    string                    `json:"eligibilityStatus"`
	BenefitsInformation  []stediBenefitInfo        `json:"benefitsInformation"`
}

type stediBenefitInfo struct {
	ServiceType string `json:"serviceType"`
	Status      string `json:"status"`
}

// StediEligibility checks patient eligibility via the Stedi REST JSON API.
// It covers BCBS, Medicare, Medicaid, and all commercial payers through
// a single API endpoint.
type StediEligibility struct {
	apiKey            string
	tradingPartnerId  string
	providerNpi       string
	providerTaxId     string
	configured        bool
	ctx               context.Context
	httpClient        *http.Client
}

// GetPluginName returns the Java-style dotted class name of this plugin.
func (s *StediEligibility) GetPluginName() string {
	return StediPluginName
}

// GetPluginVersion returns the plugin version.
func (s *StediEligibility) GetPluginVersion() string {
	return StediPluginVersion
}

// GetPluginConfigurationOptions returns the names of user-configurable
// options required by this plugin.
func (s *StediEligibility) GetPluginConfigurationOptions() []string {
	return stediConfigKeys
}

// SetContext stores the execution context for use by this plugin.
func (s *StediEligibility) SetContext(ctx context.Context) error {
	s.ctx = ctx
	return nil
}

// CheckEligibility loads configuration, builds a JSON request body,
// POSTs it to the Stedi API, and returns the parsed eligibility response.
func (s *StediEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	if !s.configured {
		if err := s.loadConfig(userName); err != nil {
			return nil, fmt.Errorf("stedi: config: %w", err)
		}
	}

	// Build the JSON request body.
	body, err := s.buildRequestBody(values)
	if err != nil {
		return nil, fmt.Errorf("stedi: build request: %w", err)
	}

	// Create the HTTP request.
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, StediAPIEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("stedi: create request: %w", err)
	}
	req.Header.Set("Authorization", s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Ensure we have a valid HTTP client.
	if s.httpClient == nil {
		s.httpClient = &http.Client{Timeout: StediTimeout}
	}

	// Execute the request.
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &EligibilityResponse{
			Status:      StatusOK,
			SuccessCode: SuccessCodeValidationFailure,
			Messages:    []string{fmt.Sprintf("Stedi API request failed: %v", err)},
		}, nil
	}
	defer resp.Body.Close()

	// Read the response body.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &EligibilityResponse{
			Status:      StatusOK,
			SuccessCode: SuccessCodeValidationFailure,
			Messages:    []string{fmt.Sprintf("Failed to read Stedi response: %v", err)},
		}, nil
	}

	// Check for non-200 status codes.
	if resp.StatusCode != http.StatusOK {
		return &EligibilityResponse{
			Status:      StatusOK,
			SuccessCode: SuccessCodeValidationFailure,
			Messages:    []string{fmt.Sprintf("Stedi API returned %d: %s", resp.StatusCode, string(respBody))},
		}, nil
	}

	// Parse the Stedi response.
	return s.parseResponse(respBody)
}

// loadConfig reads Stedi configuration from tUserConfig filtered by namespace.
func (s *StediEligibility) loadConfig(userName string) error {
	if model.Queries == nil {
		return fmt.Errorf("database not initialized")
	}

	configValues, err := model.GetConfigValues(userName)
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}

	params := make(map[string]string)
	for _, cv := range configValues {
		if cv.Namespace == StediConfigNS {
			params[cv.Option] = cv.Value
		}
	}

	s.apiKey = params["stediApiKey"]
	s.tradingPartnerId = params["stediTradingPartnerId"]
	s.providerNpi = params["stediProviderNpi"]
	s.providerTaxId = params["stediProviderTaxId"]

	if s.apiKey == "" {
		return fmt.Errorf("stediApiKey not configured")
	}
	if s.tradingPartnerId == "" {
		return fmt.Errorf("stediTradingPartnerId not configured")
	}
	if s.providerNpi == "" {
		return fmt.Errorf("stediProviderNpi not configured")
	}

	s.configured = true
	return nil
}

// buildRequestBody constructs the JSON request body for the Stedi API
// from configuration and input values.
func (s *StediEligibility) buildRequestBody(values map[string]string) ([]byte, error) {
	// Parse service type codes from comma-separated string.
	var serviceTypeCodes []string
	if raw := strings.TrimSpace(values["serviceTypeCodes"]); raw != "" {
		parts := strings.Split(raw, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				serviceTypeCodes = append(serviceTypeCodes, p)
			}
		}
	}

	req := stediRequest{
		TradingPartnerServiceID: s.tradingPartnerId,
		Provider: stediProvider{
			NPI:              s.providerNpi,
			OrganizationName: strings.TrimSpace(values["organizationName"]),
		},
		Subscriber: stediSubscriber{
			MemberID:    strings.TrimSpace(values["memberId"]),
			FirstName:   strings.TrimSpace(values["firstName"]),
			LastName:    strings.TrimSpace(values["lastName"]),
			DateOfBirth: strings.TrimSpace(values["dateOfBirth"]),
		},
		ServiceTypeCodes: serviceTypeCodes,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return data, nil
}

// parseResponse converts the Stedi JSON response into an EligibilityResponse.
func (s *StediEligibility) parseResponse(data []byte) (*EligibilityResponse, error) {
	var sr stediResponse
	if err := json.Unmarshal(data, &sr); err != nil {
		return &EligibilityResponse{
			Status:      StatusOK,
			SuccessCode: SuccessCodeValidationFailure,
			Messages:    []string{fmt.Sprintf("Failed to parse Stedi response: %v", err)},
		}, nil
	}

	status := strings.ToLower(strings.TrimSpace(sr.Subscriber.EligibilityStatus))

	var messages []string
	for _, bi := range sr.Subscriber.BenefitsInformation {
		messages = append(messages, fmt.Sprintf("%s: %s", bi.ServiceType, bi.Status))
	}

	if status == "active" {
		return &EligibilityResponse{
			Status:      StatusOK,
			SuccessCode: SuccessCodeSuccess,
			Messages:    messages,
		}, nil
	}

	// Non-active status or unknown.
	if len(messages) == 0 {
		messages = []string{fmt.Sprintf("Eligibility status: %s", sr.Subscriber.EligibilityStatus)}
	}
	return &EligibilityResponse{
		Status:      StatusOK,
		SuccessCode: SuccessCodeValidationFailure,
		Messages:    messages,
	}, nil
}
