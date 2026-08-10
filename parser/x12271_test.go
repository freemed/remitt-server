package parser

import (
	"encoding/json"
	"testing"
)

// fullX12271Sample is a realistic X12 271 eligibility response with subscriber
// and dependent, multiple EB segments, DTP, MSG, AAA.
const fullX12271Sample = `ISA*00*          *00*          *ZZ*SENDERID       *ZZ*RECEIVERID     *230801*1200*^*00501*000000001*0*P*:~
GS*HB*SENDERID*RECEIVERID*20230801*1200*1*X*005010X279A1~
ST*271*0001*005010X279A1~
BHT*0022*11*TRACE123*20230801*1200~
HL*1**20*1~
NM1*PR*2*BLUE CROSS*****PI*PAYERID~
HL*2*1*21*1~
NM1*1P*2*PROVIDER NAME*****XX*1234567890~
HL*3*2*22*0~
TRN*1*TRACE456*9*ABC~
NM1*IL*1*DOE*JOHN****MI*MEMBER123~
REF*1L*GROUP123~
DMG*D8*19800101~
DTP*346*D8*20230101~
EB*1**30*HM*HEALTH PLAN*27*500~
EB*B**33*HM*COPAY*27*25~
EB*C**48*HM*DEDUCTIBLE*27*1000~
EB*R**98*HM*PHYSICIAN~
MSG*GENERAL ELIGIBILITY INFORMATION~
MSG*ADDITIONAL MESSAGE TEXT~
HL*4*3*23*0~
TRN*1*TRACE789*9*DEF~
NM1*QC*1*DOE*JANE****MI*MEMBER456~
REF*1L*GROUP456~
DMG*D8*19850315~
DTP*346*D8*20230101~
EB*1**30*HM*HEALTH PLAN*27*500~
EB*D**86*HM*COINSURANCE*27*20~
AAA*N**15*P~
SE*26*0001~
GE*1*1~
IEA*1*000000001~
`

func TestX12271Parser_ParseData_Subscriber(t *testing.T) {
	p := &X12271Parser{}

	result, err := p.ParseData(fullX12271Sample)
	if err != nil {
		t.Fatalf("ParseData returned error: %v", err)
	}

	var resp X12271Response
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Verify subscriber
	if resp.Subscriber.LastName != "DOE" {
		t.Errorf("expected subscriber LastName 'DOE', got '%s'", resp.Subscriber.LastName)
	}
	if resp.Subscriber.FirstName != "JOHN" {
		t.Errorf("expected subscriber FirstName 'JOHN', got '%s'", resp.Subscriber.FirstName)
	}
	if resp.Subscriber.MemberID != "MEMBER123" {
		t.Errorf("expected subscriber MemberID 'MEMBER123', got '%s'", resp.Subscriber.MemberID)
	}
	if resp.Subscriber.TraceNumber != "TRACE456" {
		t.Errorf("expected subscriber TraceNumber 'TRACE456', got '%s'", resp.Subscriber.TraceNumber)
	}
	if resp.Subscriber.DateOfBirth != "19800101" {
		t.Errorf("expected subscriber DateOfBirth '19800101', got '%s'", resp.Subscriber.DateOfBirth)
	}

	// Verify subscriber benefits
	if len(resp.Subscriber.Benefits) != 4 {
		t.Fatalf("expected 4 subscriber benefits, got %d", len(resp.Subscriber.Benefits))
	}

	benefit0 := resp.Subscriber.Benefits[0]
	if benefit0.EligibilityCode != "1" {
		t.Errorf("expected benefit[0].EligibilityCode '1', got '%s'", benefit0.EligibilityCode)
	}
	if benefit0.ServiceType != "30" {
		t.Errorf("expected benefit[0].ServiceType '30', got '%s'", benefit0.ServiceType)
	}
	if benefit0.InsuranceType != "HM" {
		t.Errorf("expected benefit[0].InsuranceType 'HM', got '%s'", benefit0.InsuranceType)
	}
	if benefit0.PlanDescription != "HEALTH PLAN" {
		t.Errorf("expected benefit[0].PlanDescription 'HEALTH PLAN', got '%s'", benefit0.PlanDescription)
	}
	if benefit0.TimePeriodQual != "27" {
		t.Errorf("expected benefit[0].TimePeriodQual '27', got '%s'", benefit0.TimePeriodQual)
	}
	if benefit0.BenefitAmount != 500 {
		t.Errorf("expected benefit[0].BenefitAmount 500, got %f", benefit0.BenefitAmount)
	}

	// Verify subscriber messages
	if len(benefit0.Messages) != 2 {
		t.Errorf("expected 2 messages on first benefit, got %d", len(benefit0.Messages))
	}
	if benefit0.Messages[0] != "GENERAL ELIGIBILITY INFORMATION" {
		t.Errorf("expected message[0] 'GENERAL ELIGIBILITY INFORMATION', got '%s'", benefit0.Messages[0])
	}
	if benefit0.Messages[1] != "ADDITIONAL MESSAGE TEXT" {
		t.Errorf("expected message[1] 'ADDITIONAL MESSAGE TEXT', got '%s'", benefit0.Messages[1])
	}

	// Verify active coverage detection (EB01=1)
	if !resp.RequestValid {
		t.Errorf("expected RequestValid to be true for active coverage")
	}
}

func TestX12271Parser_ParseData_Dependent(t *testing.T) {
	p := &X12271Parser{}

	result, err := p.ParseData(fullX12271Sample)
	if err != nil {
		t.Fatalf("ParseData returned error: %v", err)
	}

	var resp X12271Response
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Verify dependent exists
	if len(resp.Dependents) != 1 {
		t.Fatalf("expected 1 dependent, got %d", len(resp.Dependents))
	}

	dep := resp.Dependents[0]
	if dep.LastName != "DOE" {
		t.Errorf("expected dependent LastName 'DOE', got '%s'", dep.LastName)
	}
	if dep.FirstName != "JANE" {
		t.Errorf("expected dependent FirstName 'JANE', got '%s'", dep.FirstName)
	}
	if dep.MemberID != "MEMBER456" {
		t.Errorf("expected dependent MemberID 'MEMBER456', got '%s'", dep.MemberID)
	}
	if dep.TraceNumber != "TRACE789" {
		t.Errorf("expected dependent TraceNumber 'TRACE789', got '%s'", dep.TraceNumber)
	}
	if dep.DateOfBirth != "19850315" {
		t.Errorf("expected dependent DateOfBirth '19850315', got '%s'", dep.DateOfBirth)
	}
}

func TestX12271Parser_ParseData_DependentBenefits(t *testing.T) {
	p := &X12271Parser{}

	result, err := p.ParseData(fullX12271Sample)
	if err != nil {
		t.Fatalf("ParseData returned error: %v", err)
	}

	var resp X12271Response
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(resp.Dependents) == 0 {
		t.Fatal("expected at least one dependent")
	}

	dep := resp.Dependents[0]
	if len(dep.Benefits) != 2 {
		t.Fatalf("expected 2 dependent benefits, got %d", len(dep.Benefits))
	}

	// First dependent benefit: EB*1**30*HM*HEALTH PLAN*27*500
	b0 := dep.Benefits[0]
	if b0.EligibilityCode != "1" {
		t.Errorf("expected dep benefit[0].EligibilityCode '1', got '%s'", b0.EligibilityCode)
	}
	if b0.BenefitAmount != 500 {
		t.Errorf("expected dep benefit[0].BenefitAmount 500, got %f", b0.BenefitAmount)
	}

	// Second dependent benefit: EB*D**86*HM*COINSURANCE*27*20
	b1 := dep.Benefits[1]
	if b1.EligibilityCode != "D" {
		t.Errorf("expected dep benefit[1].EligibilityCode 'D', got '%s'", b1.EligibilityCode)
	}
	if b1.BenefitAmount != 20 {
		t.Errorf("expected dep benefit[1].BenefitAmount 20, got %f", b1.BenefitAmount)
	}
}

func TestX12271Parser_ParseData_AAAErrors(t *testing.T) {
	p := &X12271Parser{}

	result, err := p.ParseData(fullX12271Sample)
	if err != nil {
		t.Fatalf("ParseData returned error: %v", err)
	}

	var resp X12271Response
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(resp.AAAErrors) != 1 {
		t.Fatalf("expected 1 AAA error, got %d", len(resp.AAAErrors))
	}
	if resp.AAAErrors[0] != "N**15" {
		t.Errorf("expected AAA error 'N**15', got '%s'", resp.AAAErrors[0])
	}
}

func TestX12271Parser_ParseData_Empty(t *testing.T) {
	p := &X12271Parser{}

	result, err := p.ParseData("")
	if err != nil {
		t.Fatalf("ParseData returned error: %v", err)
	}

	if result != "{}" && result != "{\n}" {
		t.Errorf("expected empty result to be '{}', got '%s'", result)
	}
}

func TestX12271Parser_ParseData_NoBenefits(t *testing.T) {
	sample := `ISA*00*          *00*          *ZZ*SENDERID       *ZZ*RECEIVERID     *230801*1200*^*00501*000000001*0*P*:~
GS*HB*SENDERID*RECEIVERID*20230801*1200*1*X*005010X279A1~
ST*271*0001*005010X279A1~
BHT*0022*11*TRACE123*20230801*1200~
HL*1**20*1~
NM1*PR*2*PAYER*****PI*PAYERID~
HL*2*1*21*1~
NM1*1P*2*PROVIDER*****XX*1234567890~
HL*3*2*22*0~
NM1*IL*1*SMITH*BOB~
AAA*N**42*C~
SE*11*0001~
GE*1*1~
IEA*1*000000001~
`

	p := &X12271Parser{}

	result, err := p.ParseData(sample)
	if err != nil {
		t.Fatalf("ParseData returned error: %v", err)
	}

	var resp X12271Response
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Should have AAA error
	if len(resp.AAAErrors) != 1 {
		t.Fatalf("expected 1 AAA error, got %d", len(resp.AAAErrors))
	}
	if resp.AAAErrors[0] != "N**42" {
		t.Errorf("expected AAA error 'N**42', got '%s'", resp.AAAErrors[0])
	}

	// Should NOT have active coverage
	if resp.RequestValid {
		t.Errorf("expected RequestValid to be false when no active coverage EB*1")
	}

	// Subscriber should exist with no benefits
	if resp.Subscriber.LastName != "SMITH" {
		t.Errorf("expected subscriber LastName 'SMITH', got '%s'", resp.Subscriber.LastName)
	}
	if len(resp.Subscriber.Benefits) != 0 {
		t.Errorf("expected 0 benefits, got %d", len(resp.Subscriber.Benefits))
	}
}

func TestX12271Parser_SetContext(t *testing.T) {
	p := &X12271Parser{}

	// Should accept nil context
	if err := p.SetContext(nil); err != nil {
		t.Errorf("SetContext(nil) should not error: %v", err)
	}
}
