package parser

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/freemed/remitt-server/model"
)

func init() {
	RegisterParser("X12835Parser", func() Parser { return &X12835Parser{} })
}

// X12835Parser parses X12 835 remittance advice data.
type X12835Parser struct {
	ctx context.Context
}

func (p *X12835Parser) ParseData(data string) (string, error) {
	// Auto-detect delimiter
	delimiter := "*"
	if len(data) > 3 {
		delimiter = string(data[3])
	}

	segments := strings.Split(data, "~")

	remit := model.X12Remittance{}
	var currentClaim *model.X12ClaimPayment
	var currentProvider *model.X12ProviderClaimGroup

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		elements := strings.Split(seg, delimiter)
		segID := elements[0]

		switch segID {
		case "BPR":
			// Financial information
			if len(elements) >= 3 {
				remit.CheckAmount = parseFloat(elements[2])
			}
			if len(elements) >= 4 {
				remit.CreditAmount = parseFloat(elements[3])
			}
			if len(elements) >= 5 {
				remit.CheckNumber = elements[4]
			}
			if len(elements) >= 17 {
				remit.CheckDate = elements[16]
			}

		case "TRN":
			// Trace number
			if len(elements) >= 3 {
				remit.CheckNumber = elements[2] // override with TRN02
			}

		case "N1":
			if len(elements) < 3 {
				continue
			}
			entity := model.X12Identification{Qualifier: elements[2], ID: elem(elements, 3)}
			switch elements[1] {
			case "PE": // Payee
				if remit.Payee == nil {
					remit.Payee = &model.X12Payee{}
				}
				remit.Payee.Name = elem(elements, 2)
				remit.Payee.Identification = append(remit.Payee.Identification, entity)
			case "PR": // Payer
				if remit.Payer == nil {
					remit.Payer = &model.X12Payer{}
				}
				remit.Payer.Name = elem(elements, 2)
				remit.Payer.Identification = append(remit.Payer.Identification, entity)
			}

		case "LX":
			// New provider group
			currentProvider = &model.X12ProviderClaimGroup{}
			remit.ProviderClaimGroups = append(remit.ProviderClaimGroups, *currentProvider)
			currentProvider = &remit.ProviderClaimGroups[len(remit.ProviderClaimGroups)-1]

		case "CLP":
			if currentProvider == nil {
				currentProvider = &model.X12ProviderClaimGroup{}
				remit.ProviderClaimGroups = append(remit.ProviderClaimGroups, *currentProvider)
				currentProvider = &remit.ProviderClaimGroups[len(remit.ProviderClaimGroups)-1]
			}
			currentClaim = &model.X12ClaimPayment{}
			if len(elements) >= 2 {
				currentClaim.PatientControlNumber = elements[1]
			}
			if len(elements) >= 3 {
				currentClaim.ClaimStatus = elements[2]
			}
			if len(elements) >= 4 {
				currentClaim.ClaimChargeAmount = parseFloat(elements[3])
			}
			if len(elements) >= 5 {
				currentClaim.PaidAmount = parseFloat(elements[4])
			}
			if len(elements) >= 6 {
				currentClaim.PatientResponsibilityAmt = parseFloat(elements[5])
			}
			if len(elements) >= 7 {
				currentClaim.PayerClaimControlNumber = elements[6]
			}
			currentProvider.Claims = append(currentProvider.Claims, *currentClaim)

		case "CAS":
			if currentClaim == nil {
				continue
			}
			// CAS segment has group/reason/amount triplets starting at element 1
			for i := 1; i+2 < len(elements); i += 3 {
				adj := model.X12ClaimAdjustment{
					GroupCode:  elements[i],
					ReasonCode: elem(elements, i+1),
					Amount:     parseFloat(elem(elements, i+2)),
				}
				currentClaim.ClaimAdjustments = append(currentClaim.ClaimAdjustments, adj)
			}

		case "NM1":
			if currentProvider == nil {
				continue
			}
			if len(elements) < 4 {
				continue
			}
			if elements[1] == "82" { // Rendering provider
				currentProvider.ProviderNPI = elem(elements, 9)
			}
		}
	}

	result, err := json.MarshalIndent(remit, "", "  ")
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func (p *X12835Parser) SetContext(ctx context.Context) error {
	p.ctx = ctx
	return nil
}

// parseFloat converts a string to float64, returning 0 on error.
func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
