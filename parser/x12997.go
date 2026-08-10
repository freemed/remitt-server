package parser

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/freemed/remitt-server/model"
)

func init() {
	RegisterParser("org.remitt.plugin.parser.X12997Parser", func() Parser { return &X12997Parser{} })
}

// X12997Result is the top-level result of parsing a 997 functional acknowledgment.
type X12997Result struct {
	AckCode          string                  `json:"ack_code"`
	Errors           []string                `json:"errors"`
	FunctionalGroups []X12997FunctionalGroup `json:"functional_groups"`
}

// X12997FunctionalGroup holds AK1/AK9 data for a single functional group.
type X12997FunctionalGroup struct {
	GroupID            string                 `json:"group_id"`
	GroupControlNumber string                 `json:"group_control_number"`
	AckCode            string                 `json:"ack_code"`
	FunctionalIDCount  string                 `json:"functional_id_count"`
	ReceivedCount      string                 `json:"received_count"`
	AcceptedCount      string                 `json:"accepted_count"`
	Errors             []string               `json:"errors"`
	TransactionSets    []X12997TransactionSet `json:"transaction_sets"`
}

// X12997TransactionSet holds AK2/AK5 data for a single transaction set.
type X12997TransactionSet struct {
	TransactionID            string              `json:"transaction_id"`
	TransactionControlNumber string              `json:"transaction_control_number"`
	AckCode                  string              `json:"ack_code"`
	Errors                   []string            `json:"errors"`
	SegmentNotes             []X12997SegmentNote `json:"segment_notes"`
	ElementNotes             []X12997ElementNote `json:"element_notes"`
}

// X12997SegmentNote holds AK3 data for a segment-level error.
type X12997SegmentNote struct {
	SegmentID       string `json:"segment_id"`
	SegmentPosition string `json:"segment_position"`
	LoopID          string `json:"loop_id"`
	ErrorCode       string `json:"error_code"`
}

// X12997ElementNote holds AK4 data for an element-level error.
type X12997ElementNote struct {
	ElementPosition  string `json:"element_position"`
	ElementReference string `json:"element_reference"`
	ErrorCode        string `json:"error_code"`
	BadElement       string `json:"bad_element"`
}

// X12997Parser parses X12 997 functional acknowledgment data.
type X12997Parser struct {
	ctx context.Context
}

func (p *X12997Parser) ParseData(data string) (string, error) {
	// Auto-detect delimiter
	delimiter := "*"
	if len(data) > 3 {
		delimiter = string(data[3])
	}

	segments := strings.Split(data, "~")

	result := X12997Result{}

	var currentFG *X12997FunctionalGroup
	var currentTS *X12997TransactionSet

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		elements := strings.Split(seg, delimiter)
		segID := elements[0]

		switch segID {
		case "AK1":
			// New functional group response header
			currentTS = nil // reset
			fg := X12997FunctionalGroup{}
			if len(elements) >= 2 {
				fg.GroupID = elem(elements, 1)
			}
			if len(elements) >= 3 {
				fg.GroupControlNumber = elem(elements, 2)
			}
			result.FunctionalGroups = append(result.FunctionalGroups, fg)
			currentFG = &result.FunctionalGroups[len(result.FunctionalGroups)-1]

		case "AK2":
			// Transaction set response header
			if currentFG == nil {
				continue
			}
			ts := X12997TransactionSet{}
			if len(elements) >= 2 {
				ts.TransactionID = elem(elements, 1)
			}
			if len(elements) >= 3 {
				ts.TransactionControlNumber = elem(elements, 2)
			}
			currentFG.TransactionSets = append(currentFG.TransactionSets, ts)
			currentTS = &currentFG.TransactionSets[len(currentFG.TransactionSets)-1]

		case "AK3":
			// Data segment note
			if currentTS == nil {
				continue
			}
			sn := X12997SegmentNote{
				SegmentID:       elem(elements, 1),
				SegmentPosition: elem(elements, 2),
				LoopID:          elem(elements, 3),
				ErrorCode:       elem(elements, 4),
			}
			currentTS.SegmentNotes = append(currentTS.SegmentNotes, sn)

		case "AK4":
			// Data element note
			if currentTS == nil {
				continue
			}
			en := X12997ElementNote{
				ElementPosition:  elem(elements, 1),
				ElementReference: elem(elements, 2),
				ErrorCode:        elem(elements, 3),
				BadElement:       elem(elements, 4),
			}
			currentTS.ElementNotes = append(currentTS.ElementNotes, en)

		case "AK5":
			// Transaction set response trailer
			if currentTS == nil {
				continue
			}
			if len(elements) >= 2 {
				currentTS.AckCode = elem(elements, 1)
			}
			// AK502-AK505 are implementation error codes
			for i := 2; i < len(elements); i++ {
				if e := strings.TrimSpace(elements[i]); e != "" {
					currentTS.Errors = append(currentTS.Errors, e)
				}
			}

		case "AK9":
			// Functional group response trailer
			if currentFG == nil {
				continue
			}
			if len(elements) >= 2 {
				currentFG.AckCode = elem(elements, 1)
			}
			if len(elements) >= 3 {
				currentFG.FunctionalIDCount = elem(elements, 2)
			}
			if len(elements) >= 4 {
				currentFG.ReceivedCount = elem(elements, 3)
			}
			if len(elements) >= 5 {
				currentFG.AcceptedCount = elem(elements, 4)
			}
			// AK905-AK909 are error codes
			for i := 5; i < len(elements); i++ {
				if e := strings.TrimSpace(elements[i]); e != "" {
					currentFG.Errors = append(currentFG.Errors, e)
				}
			}
		}
	}

	// Populate top-level fields from the first functional group for backward compatibility
	if len(result.FunctionalGroups) > 0 {
		fg := result.FunctionalGroups[0]
		result.AckCode = fg.AckCode
		result.Errors = fg.Errors
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func (p *X12997Parser) SetContext(ctx context.Context) error {
	p.ctx = ctx
	return nil
}

// ToFunctionalAck converts the parsed result to a model.X12FunctionalAck.
func (r *X12997Result) ToFunctionalAck() model.X12FunctionalAck {
	return model.X12FunctionalAck{
		AckCode: r.AckCode,
		Errors:  r.Errors,
	}
}
