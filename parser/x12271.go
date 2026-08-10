package parser

import (
	"context"
	"encoding/json"
	"strings"
)

func init() {
	RegisterParser("org.remitt.plugin.parser.X12271Parser", func() Parser { return &X12271Parser{} })
}

// X12271Response represents a parsed X12 271 eligibility response.
type X12271Response struct {
	RequestValid bool              `json:"request_valid"`
	AAAErrors    []string          `json:"aaa_errors,omitempty"`
	Subscriber   X12271Subscriber  `json:"subscriber"`
	Dependents   []X12271Subscriber `json:"dependents,omitempty"`
}

// X12271Subscriber holds subscriber or dependent information from a 271.
type X12271Subscriber struct {
	TraceNumber string         `json:"trace_number"`
	MemberID    string         `json:"member_id"`
	FirstName   string         `json:"first_name"`
	LastName    string         `json:"last_name"`
	DateOfBirth string         `json:"date_of_birth"`
	Benefits    []X12271Benefit `json:"benefits"`
}

// X12271Benefit holds eligibility/benefit information from an EB segment.
type X12271Benefit struct {
	EligibilityCode string   `json:"eligibility_code"`
	ServiceType     string   `json:"service_type"`
	InsuranceType   string   `json:"insurance_type"`
	CoverageLevel   string   `json:"coverage_level"`
	PlanDescription string   `json:"plan_description"`
	TimePeriodQual  string   `json:"time_period_qualifier"`
	BenefitAmount   float64  `json:"benefit_amount"`
	InPlanNetwork   string   `json:"in_plan_network"`
	Messages        []string `json:"messages"`
}

// X12271Parser parses X12 271 eligibility response data.
type X12271Parser struct {
	ctx context.Context
}

func (p *X12271Parser) ParseData(data string) (string, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return "{}", nil
	}

	// Auto-detect delimiter from ISA segment (position 3, after "ISA")
	delimiter := "*"
	if len(data) > 3 {
		delimiter = string(data[3])
	}

	segments := strings.Split(data, "~")

	resp := X12271Response{}

	var currentSub *X12271Subscriber      // current subscriber being built
	var currentDep *X12271Subscriber       // current dependent being built
	var currentPerson *X12271Subscriber    // active person receiving EB/DTP/MSG segments
	var currentHL int                      // current HL level (22=subscriber, 23=dependent)
	var pendingMessages []string           // MSG segments collected before next EB

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}

		elements := strings.Split(seg, delimiter)
		segID := elements[0]

		switch segID {
		case "ISA", "GS", "ST", "BHT", "SE", "GE", "IEA":
			// Envelope segments — skip
			continue

		case "HL":
			// Hierarchical Level: HL*ID*ParentID*LevelCode*ChildCode
			// Flush pending messages to the first benefit of current person
			if currentPerson != nil && len(pendingMessages) > 0 && len(currentPerson.Benefits) > 0 {
				currentPerson.Benefits[0].Messages = append(
					currentPerson.Benefits[0].Messages, pendingMessages...)
				pendingMessages = nil
			}

			hlLevel := elem(elements, 3)
			switch hlLevel {
			case "22":
				// Subscriber
				currentSub = &X12271Subscriber{}
				currentPerson = currentSub
				currentDep = nil
				currentHL = 22
				resp.Subscriber = *currentSub
			case "23":
				// Dependent
				currentDep = &X12271Subscriber{}
				currentPerson = currentDep
				currentSub = nil
				currentHL = 23
				resp.Dependents = append(resp.Dependents, *currentDep)
			default:
				// Other levels (20=Information Source, 21=Information Receiver)
				// Reset person tracking but keep the reference to add back later
				currentPerson = nil
				currentHL = 0
			}

		case "NM1":
			// Individual or Organizational Name
			if currentSub != nil && currentHL == 22 && elem(elements, 1) == "IL" {
				// Subscriber name
				currentSub.LastName = elem(elements, 3)
				currentSub.FirstName = elem(elements, 4)
				currentSub.MemberID = elem(elements, 9)
				resp.Subscriber = *currentSub
			} else if currentDep != nil && currentHL == 23 && elem(elements, 1) == "QC" {
				// Dependent name
				currentDep.LastName = elem(elements, 3)
				currentDep.FirstName = elem(elements, 4)
				currentDep.MemberID = elem(elements, 9)
				// Update in dependents slice
				resp.Dependents[len(resp.Dependents)-1] = *currentDep
			}

		case "TRN":
			// Trace Number
			traceNum := elem(elements, 2)
			if currentHL == 22 {
				// Update both pointer and struct copy
				if currentSub != nil {
					currentSub.TraceNumber = traceNum
					resp.Subscriber = *currentSub
				}
			} else if currentHL == 23 && currentDep != nil {
				currentDep.TraceNumber = traceNum
				resp.Dependents[len(resp.Dependents)-1] = *currentDep
			}

		case "REF":
			// Reference Identification
			// REF*1L = Group/Policy Number, REF*SY = Subscriber ID, etc.
			refQual := elem(elements, 1)
			refValue := elem(elements, 2)
			if refQual == "SY" || refQual == "EJ" {
				// Subscriber/Member identifier
				if currentHL == 22 && currentSub != nil {
					currentSub.MemberID = refValue
					resp.Subscriber = *currentSub
				} else if currentHL == 23 && currentDep != nil {
					currentDep.MemberID = refValue
					resp.Dependents[len(resp.Dependents)-1] = *currentDep
				}
			}

		case "DMG":
			// Demographic Information
			dob := elem(elements, 2) // DMG02 is date of birth (format in DMG01)
			if dob != "" {
				if currentHL == 22 && currentSub != nil {
					currentSub.DateOfBirth = dob
					resp.Subscriber = *currentSub
				} else if currentHL == 23 && currentDep != nil {
					currentDep.DateOfBirth = dob
					resp.Dependents[len(resp.Dependents)-1] = *currentDep
				}
			}

		case "DTP":
			// Date/Time Period — associate with current person only
			// (DTP is tracked at person level via benefits context)
			// For now, DTP is informational and already captured implicitly.
			_ = elem(elements, 1) // date/time qualifier
			_ = elem(elements, 2) // date/time format qualifier
			_ = elem(elements, 3) // date/time period

		case "EB":
			// Eligibility/Benefit Information
			if currentPerson == nil {
				continue
			}

			// Flush pending messages to the first benefit
			if currentPerson != nil && len(pendingMessages) > 0 && len(currentPerson.Benefits) > 0 {
				currentPerson.Benefits[0].Messages = append(
					currentPerson.Benefits[0].Messages, pendingMessages...)
				pendingMessages = nil
			}

			benefit := X12271Benefit{
				EligibilityCode: elem(elements, 1),
				CoverageLevel:   elem(elements, 2),
				ServiceType:     elem(elements, 3),
				InsuranceType:   elem(elements, 4),
				PlanDescription: elem(elements, 5),
				TimePeriodQual:  elem(elements, 6),
				BenefitAmount:   parseFloat(elem(elements, 7)),
				InPlanNetwork:   elem(elements, 12),
			}

			currentPerson.Benefits = append(currentPerson.Benefits, benefit)

			// Update response struct copies
			if currentHL == 22 && currentSub != nil {
				resp.Subscriber = *currentSub
			} else if currentHL == 23 && currentDep != nil {
				resp.Dependents[len(resp.Dependents)-1] = *currentDep
			}

		case "MSG":
			// Message Text — collect for current person
			msg := elements[1]
			if msg != "" {
				if currentPerson != nil && len(currentPerson.Benefits) > 0 {
					// Attach to the FIRST benefit (primary eligibility entry)
					currentPerson.Benefits[0].Messages = append(
						currentPerson.Benefits[0].Messages, msg)
					// Sync back to response struct
					if currentHL == 22 && currentSub != nil {
						resp.Subscriber = *currentSub
					} else if currentHL == 23 && currentDep != nil {
						resp.Dependents[len(resp.Dependents)-1] = *currentDep
					}
				} else {
					// Queue for the next benefit
					pendingMessages = append(pendingMessages, msg)
				}
			}

		case "AAA":
			// Request Validation
			// AAA*YesNo*RejectReason*FollowUp*
			yesNo := elem(elements, 1)
			reason := elem(elements, 3) // AAA03 = Follow-up Action Code

			if yesNo == "N" {
				resp.AAAErrors = append(resp.AAAErrors, yesNo+"**"+reason)
			}
		}
	}

	// Flush any remaining pending messages to the first benefit
	if currentPerson != nil && len(pendingMessages) > 0 && len(currentPerson.Benefits) > 0 {
		currentPerson.Benefits[0].Messages = append(
			currentPerson.Benefits[0].Messages, pendingMessages...)
	}

	// Determine RequestValid based on active coverage (EB*1 in any person)
	if !resp.RequestValid {
		checkCoverage := func(person X12271Subscriber) bool {
			for _, b := range person.Benefits {
				if b.EligibilityCode == "1" {
					return true
				}
			}
			return false
		}
		if checkCoverage(resp.Subscriber) {
			resp.RequestValid = true
		}
		for _, dep := range resp.Dependents {
			if checkCoverage(dep) {
				resp.RequestValid = true
				break
			}
		}
		// If still not valid but AAA*Y was seen, mark valid
		if !resp.RequestValid {
			resp.RequestValid = len(resp.AAAErrors) == 0
		}
	}

	result, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func (p *X12271Parser) SetContext(ctx context.Context) error {
	p.ctx = ctx
	return nil
}

// parseFloat converts a string to float64, returning 0 on error.
// Defined in x12835.go but redefined here for self-contained readability.
