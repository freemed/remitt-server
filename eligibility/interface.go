package eligibility

import "context"

// Status constants for EligibilityResponse.
const (
	StatusOK = "OK"
)

// SuccessCode constants.
const (
	SuccessCodeSuccess           = "SUCCESS"
	SuccessCodeValidationFailure = "VALIDATION_FAILURE"
)

// EligibilityRequest is the request payload for eligibility checks.
type EligibilityRequest struct {
	Plugin  string            `json:"plugin"`
	Request map[string]string `json:"request"`
}

// EligibilityResponse holds the result of an eligibility check.
type EligibilityResponse struct {
	Status      string   `json:"status"`
	SuccessCode string   `json:"successCode"`
	Messages    []string `json:"messages"`
}

// EligibilityChecker is the interface for eligibility check plugins.
type EligibilityChecker interface {
	CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error)
	GetPluginName() string
	GetPluginVersion() string
	GetPluginConfigurationOptions() []string
	SetContext(context.Context) error
}
