package eligibility

import "context"

// Status constants for EligibilityResponse.
//
// These mirror the Java 0.5.x org.remitt.prototype.EligibilityStatus enum —
// the DB/API contract is fixed, so the Go values must match the Java ones
// exactly. Only StatusOK was defined before; the other four were missing.
const (
	StatusOK           = "OK"
	StatusBad          = "BAD"
	StatusContinuation = "CONTINUATION"
	StatusServerError  = "SERVER_ERROR"
	StatusProcessing   = "PROCESSING"
)

// SuccessCode constants.
//
// These mirror the Java 0.5.x org.remitt.prototype.EligibilitySuccessCode
// enum. Only the first two were defined before; the remaining six are what
// real payer gateways actually return (see the Java GatewayEDIEligibility
// response mapping), so a plugin must be able to report them.
const (
	SuccessCodeSuccess                    = "SUCCESS"
	SuccessCodeValidationFailure          = "VALIDATION_FAILURE"
	SuccessCodePayerTimeout               = "PAYER_TIMEOUT"
	SuccessCodePayerNotSupported          = "PAYER_NOT_SUPPORTED"
	SuccessCodeSystemError                = "SYSTEM_ERROR"
	SuccessCodePayerEnrollmentRequired    = "PAYER_ENROLLMENT_REQUIRED"
	SuccessCodeProviderEnrollmentRequired = "PROVIDER_ENROLLMENT_REQUIRED"
	SuccessCodeProductRequired            = "PRODUCT_REQUIRED"
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
