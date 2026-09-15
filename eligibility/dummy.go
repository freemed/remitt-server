package eligibility

import (
	"context"
	"math/rand"
)

func init() {
	// The database stores the Java FQCN (tPlugins:229,
	// org.remitt.plugin.eligibility.DummyEligibility) and the SOAP layer submits
	// the short name (soap/soap_test.go), so BOTH must resolve - the same defect
	// class that left the translation registry unable to answer to the values in
	// tTranslation.
	RegisterChecker("DummyEligibility", func() EligibilityChecker { return &DummyEligibility{} })
	RegisterChecker("org.remitt.plugin.eligibility.DummyEligibility", func() EligibilityChecker { return &DummyEligibility{} })
}

type DummyEligibility struct {
	ctx context.Context
}

func (d *DummyEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	r := &EligibilityResponse{
		Status: StatusOK,
	}

	if rand.Float64() >= 0.5 {
		r.SuccessCode = SuccessCodeSuccess
		r.Messages = []string{"DUMMY BACKEND APPROVES!"}
	} else {
		r.SuccessCode = SuccessCodeValidationFailure
		r.Messages = []string{"DUMMY BACKEND DISAPPROVES!"}
	}

	return r, nil
}

func (d *DummyEligibility) GetPluginName() string {
	return "DummyEligibility"
}

func (d *DummyEligibility) GetPluginVersion() string {
	return "0.1"
}

func (d *DummyEligibility) GetPluginConfigurationOptions() []string {
	return nil
}

func (d *DummyEligibility) SetContext(ctx context.Context) error {
	d.ctx = ctx
	return nil
}
