package model

// X12Address represents an X12 N3/N4 address.
type X12Address struct {
	Addr1 string `json:"addr1"`
	Addr2 string `json:"addr2"`
	City  string `json:"city"`
	State string `json:"state"`
	Zip   string `json:"zip"`
}

// X12Amount represents an amount with qualifier.
type X12Amount struct {
	Amount    float64 `json:"amount"`
	Qualifier string  `json:"qualifier"`
}

// X12Identification represents an X12 identification segment.
type X12Identification struct {
	Qualifier string `json:"qualifier"`
	ID        string `json:"id"`
}

// X12Insured represents an insured person.
type X12Insured struct {
	LastName       string              `json:"last_name"`
	FirstName      string              `json:"first_name"`
	MiddleName     string              `json:"middle_name"`
	Suffix         string              `json:"suffix"`
	Identification []X12Identification `json:"identification"`
}

// X12Patient represents a patient.
type X12Patient struct {
	LastName       string              `json:"last_name"`
	FirstName      string              `json:"first_name"`
	MiddleName     string              `json:"middle_name"`
	Suffix         string              `json:"suffix"`
	Identification []X12Identification `json:"identification"`
}

// X12Payee represents the payee.
type X12Payee struct {
	Name           string              `json:"name"`
	Identification []X12Identification `json:"identification"`
	Address        *X12Address         `json:"address,omitempty"`
}

// X12Payer represents the payer.
type X12Payer struct {
	Name           string              `json:"name"`
	Identification []X12Identification `json:"identification"`
	Address        *X12Address         `json:"address,omitempty"`
}

// X12ClaimAdjustment represents a claim adjustment.
type X12ClaimAdjustment struct {
	GroupCode  string  `json:"group_code"`
	ReasonCode string  `json:"reason_code"`
	Amount     float64 `json:"amount"`
	Quantity   float64 `json:"quantity"`
}

// X12ClaimInformation represents claim information from CLP segment.
type X12ClaimInformation struct {
	ClaimStatus               string               `json:"claim_status"`
	ClaimNumber               string               `json:"claim_number"`
	PatientControlNumber      string               `json:"patient_control_number"`
	BillingProviderLastName   string               `json:"billing_provider_last_name"`
	BillingProviderFirstName  string               `json:"billing_provider_first_name"`
	BillingProviderMiddleName string               `json:"billing_provider_middle_name"`
	BillingProviderTaxID      string               `json:"billing_provider_tax_id"`
	BillingProviderNPI        string               `json:"billing_provider_npi"`
	ServiceDateFrom           string               `json:"service_date_from"`
	ServiceDateTo             string               `json:"service_date_to"`
	ClaimChargeAmount         float64              `json:"claim_charge_amount"`
	PatientResponsibilityAmt  float64              `json:"patient_responsibility_amount"`
	ClaimPaymentAmount        float64              `json:"claim_payment_amount"`
	PatientPaidAmount         float64              `json:"patient_paid_amount"`
	ClaimAdjustments          []X12ClaimAdjustment `json:"claim_adjustments"`
}

// X12ClaimPayment represents a claim payment.
type X12ClaimPayment struct {
	PayerClaimControlNumber  string               `json:"payer_claim_control_number"`
	PatientControlNumber     string               `json:"patient_control_number"`
	ClaimNumber              string               `json:"claim_number"`
	ClaimStatus              string               `json:"claim_status"`
	CheckNumber              string               `json:"check_number"`
	CheckDate                string               `json:"check_date"`
	BilledAmount             float64              `json:"billed_amount"`
	ClaimChargeAmount        float64              `json:"claim_charge_amount"`
	PaidAmount               float64              `json:"paid_amount"`
	PatientResponsibilityAmt float64              `json:"patient_responsibility_amount"`
	ClaimAdjustments         []X12ClaimAdjustment `json:"claim_adjustments"`
}

// X12ProviderClaimGroup represents a group of claims for one provider.
type X12ProviderClaimGroup struct {
	ProviderTaxID string            `json:"provider_tax_id"`
	ProviderNPI   string            `json:"provider_npi"`
	Claims        []X12ClaimPayment `json:"claims"`
}

// X12Remittance represents a remittance advice.
type X12Remittance struct {
	Payer               *X12Payer               `json:"payer,omitempty"`
	Payee               *X12Payee               `json:"payee,omitempty"`
	CheckNumber         string                  `json:"check_number"`
	CheckDate           string                  `json:"check_date"`
	CheckAmount         float64                 `json:"check_amount"`
	CreditAmount        float64                 `json:"credit_amount"`
	ProviderClaimGroups []X12ProviderClaimGroup `json:"provider_claim_groups"`
}

// X12TransactionSet represents a complete transaction set.
type X12TransactionSet struct {
	TraceNumber string          `json:"trace_number"`
	Payer       *X12Payer       `json:"payer,omitempty"`
	Payee       *X12Payee       `json:"payee,omitempty"`
	Remittances []X12Remittance `json:"remittances"`
}

// X12FunctionalAck represents a 997 functional acknowledgement.
type X12FunctionalAck struct {
	AckCode string   `json:"ack_code"`
	Errors  []string `json:"errors"`
}
