package parser

import (
	"context"
	"encoding/json"
	"strings"
)

func init() {
	RegisterParser("X12Parser", func() Parser { return &X12Parser{} })
}

type X12Envelope struct {
	ISA            X12ISASegment `json:"isa"`
	GS             X12GSSegment  `json:"gs"`
	ST             X12STSegment  `json:"st"`
	TransactionSet []string      `json:"transaction_set"`
	SE             X12SESegment  `json:"se"`
	GE             X12GESegment  `json:"ge"`
	IEA            X12IEASegment `json:"iea"`
}

type X12ISASegment struct {
	AuthInfoQualifier     string `json:"auth_info_qualifier"`
	AuthInfo              string `json:"auth_info"`
	SecurityInfoQualifier string `json:"security_info_qualifier"`
	SecurityInfo          string `json:"security_info"`
	SenderQualifier       string `json:"sender_qualifier"`
	SenderID              string `json:"sender_id"`
	ReceiverQualifier     string `json:"receiver_qualifier"`
	ReceiverID            string `json:"receiver_id"`
	Date                  string `json:"date"`
	Time                  string `json:"time"`
	ControlNumber         string `json:"control_number"`
	AckRequested          string `json:"ack_requested"`
	UsageIndicator        string `json:"usage_indicator"`
	ElementSeparator      string `json:"element_separator"`
}

type X12GSSegment struct {
	FunctionalIDCode string `json:"functional_id_code"`
	SenderID         string `json:"sender_id"`
	ReceiverID       string `json:"receiver_id"`
	Date             string `json:"date"`
	Time             string `json:"time"`
	ControlNumber    string `json:"control_number"`
	AgencyCode       string `json:"agency_code"`
	Version          string `json:"version"`
}

type X12STSegment struct {
	TransactionID string `json:"transaction_id"`
	ControlNumber string `json:"control_number"`
}

type X12SESegment struct {
	SegmentCount  string `json:"segment_count"`
	ControlNumber string `json:"control_number"`
}

type X12GESegment struct {
	TransactionCount string `json:"transaction_count"`
	ControlNumber    string `json:"control_number"`
}

type X12IEASegment struct {
	GroupCount    string `json:"group_count"`
	ControlNumber string `json:"control_number"`
}

type X12Parser struct {
	ctx context.Context
}

func (p *X12Parser) ParseData(data string) (string, error) {
	// Detect delimiter from ISA segment (position 3, after "ISA")
	delimiter := "*"
	segmentTerminator := "~"

	if len(data) > 3 {
		delimiter = string(data[3])
	}

	// Split into segments
	segments := strings.Split(data, segmentTerminator)

	envelope := X12Envelope{}
	transactionLines := []string{}
	inTransaction := false

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}

		elements := strings.Split(seg, delimiter)
		segID := elements[0]

		switch segID {
		case "ISA":
			if len(elements) >= 16 {
				envelope.ISA = X12ISASegment{
					AuthInfoQualifier:     elem(elements, 1),
					AuthInfo:              elem(elements, 2),
					SecurityInfoQualifier: elem(elements, 3),
					SecurityInfo:          elem(elements, 4),
					SenderQualifier:       elem(elements, 5),
					SenderID:              elem(elements, 6),
					ReceiverQualifier:     elem(elements, 7),
					ReceiverID:            elem(elements, 8),
					Date:                  elem(elements, 9),
					Time:                  elem(elements, 10),
					ControlNumber:         elem(elements, 13),
					AckRequested:          elem(elements, 14),
					UsageIndicator:        elem(elements, 15),
					ElementSeparator:      delimiter,
				}
			}
		case "GS":
			if len(elements) >= 8 {
				envelope.GS = X12GSSegment{
					FunctionalIDCode: elem(elements, 1),
					SenderID:         elem(elements, 2),
					ReceiverID:       elem(elements, 3),
					Date:             elem(elements, 4),
					Time:             elem(elements, 5),
					ControlNumber:    elem(elements, 6),
					AgencyCode:       elem(elements, 7),
					Version:          elem(elements, 8),
				}
			}
		case "ST":
			inTransaction = true
			if len(elements) >= 2 {
				envelope.ST = X12STSegment{
					TransactionID: elem(elements, 1),
					ControlNumber: elem(elements, 2),
				}
			}
		case "SE":
			inTransaction = false
			if len(elements) >= 2 {
				envelope.SE = X12SESegment{
					SegmentCount:  elem(elements, 1),
					ControlNumber: elem(elements, 2),
				}
			}
		case "GE":
			if len(elements) >= 2 {
				envelope.GE = X12GESegment{
					TransactionCount: elem(elements, 1),
					ControlNumber:    elem(elements, 2),
				}
			}
		case "IEA":
			if len(elements) >= 2 {
				envelope.IEA = X12IEASegment{
					GroupCount:    elem(elements, 1),
					ControlNumber: elem(elements, 2),
				}
			}
		default:
			if inTransaction {
				transactionLines = append(transactionLines, seg)
			}
		}
	}

	envelope.TransactionSet = transactionLines

	result, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", err
	}

	return string(result), nil
}

func (p *X12Parser) SetContext(ctx context.Context) error {
	p.ctx = ctx
	return nil
}

// elem safely retrieves a slice element by index.
func elem(s []string, i int) string {
	if i < len(s) {
		return strings.TrimSpace(s[i])
	}
	return ""
}
