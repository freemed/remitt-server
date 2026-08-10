package parser

import (
	"context"
	"encoding/json"
	"testing"
)

// sample997Accept is a clean 997 acceptance with all segments (no errors).
const sample997Accept = `ISA*00*          *00*          *ZZ*RECEIVER        *ZZ*SENDER          *240810*1200*U*00401*000000002*0*P*:~
GS*FA*RECEIVER*SENDER*20240810*1200*2*X*004010~
ST*997*0001~
AK1*HC*1~
AK2*835*0001~
AK5*A~
AK9*A*1*1*1~
SE*6*0001~
GE*1*2~
IEA*1*000000002~
`

// sample997Reject has AK3/AK4 error segments with reject codes.
const sample997Reject = `ISA*00*          *00*          *ZZ*RECEIVER        *ZZ*SENDER          *240810*1200*U*00401*000000002*0*P*:~
GS*FA*RECEIVER*SENDER*20240810*1200*2*X*004010~
ST*997*0001~
AK1*HC*1~
AK2*835*0001~
AK3*CLP*4*100*5~
AK4*2*CLP02*7*INVALID~
AK4*3*CLP03*7*999999~
AK5*R*5~
AK9*R*1*1*0~
SE*8*0001~
ST*997*0002~
AK1*HC*2~
AK2*835*0002~
AK5*A~
AK9*A*1*1*1~
SE*6*0002~
GE*2*2~
IEA*1*000000002~
`

// sample997Partial has AK9 with multiple error codes.
const sample997Partial = `ISA*00*          *00*          *ZZ*RECEIVER        *ZZ*SENDER          *240810*1200*U*00401*000000002*0*P*:~
GS*FA*RECEIVER*SENDER*20240810*1200*2*X*004010~
ST*997*0001~
AK1*HC*1~
AK2*835*0001~
AK5*E*1*2~
AK9*P*1*1*0*1*2*3~
SE*6*0001~
GE*1*2~
IEA*1*000000002~
`

func TestX12997Parser_ParseData_Accept(t *testing.T) {
	p := &X12997Parser{}
	_ = p.SetContext(context.Background())

	result, err := p.ParseData(sample997Accept)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var ack X12997Result
	if err := json.Unmarshal([]byte(result), &ack); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(ack.FunctionalGroups) != 1 {
		t.Fatalf("expected 1 functional group, got %d", len(ack.FunctionalGroups))
	}

	fg := ack.FunctionalGroups[0]
	if fg.AckCode != "A" {
		t.Errorf("expected AK9 ack code 'A', got '%s'", fg.AckCode)
	}
	if fg.FunctionalIDCount != "1" {
		t.Errorf("expected functional ID count '1', got '%s'", fg.FunctionalIDCount)
	}

	if len(fg.TransactionSets) != 1 {
		t.Fatalf("expected 1 transaction set, got %d", len(fg.TransactionSets))
	}

	ts := fg.TransactionSets[0]
	if ts.TransactionID != "835" {
		t.Errorf("expected transaction ID '835', got '%s'", ts.TransactionID)
	}
	if ts.TransactionControlNumber != "0001" {
		t.Errorf("expected control number '0001', got '%s'", ts.TransactionControlNumber)
	}
	if ts.AckCode != "A" {
		t.Errorf("expected AK5 ack code 'A', got '%s'", ts.AckCode)
	}
	if len(ts.SegmentNotes) != 0 {
		t.Errorf("expected 0 segment notes, got %d", len(ts.SegmentNotes))
	}
	if len(ts.ElementNotes) != 0 {
		t.Errorf("expected 0 element notes, got %d", len(ts.ElementNotes))
	}

	// Check the top-level X12FunctionalAck compat fields
	if ack.AckCode != "A" {
		t.Errorf("expected top-level AckCode 'A', got '%s'", ack.AckCode)
	}
}

func TestX12997Parser_ParseData_Reject(t *testing.T) {
	p := &X12997Parser{}
	_ = p.SetContext(context.Background())

	result, err := p.ParseData(sample997Reject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var ack X12997Result
	if err := json.Unmarshal([]byte(result), &ack); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(ack.FunctionalGroups) != 2 {
		t.Fatalf("expected 2 functional groups, got %d", len(ack.FunctionalGroups))
	}

	// First functional group: rejected with errors
	fg1 := ack.FunctionalGroups[0]
	if fg1.AckCode != "R" {
		t.Errorf("expected AK9 ack code 'R' in group 1, got '%s'", fg1.AckCode)
	}
	if fg1.FunctionalIDCount != "1" {
		t.Errorf("expected functional ID count '1', got '%s'", fg1.FunctionalIDCount)
	}
	if fg1.AcceptedCount != "0" {
		t.Errorf("expected accepted count '0', got '%s'", fg1.AcceptedCount)
	}
	if len(fg1.TransactionSets) != 1 {
		t.Fatalf("expected 1 transaction set in group 1, got %d", len(fg1.TransactionSets))
	}

	ts1 := fg1.TransactionSets[0]
	if ts1.AckCode != "R" {
		t.Errorf("expected AK5 ack code 'R', got '%s'", ts1.AckCode)
	}
	if len(ts1.Errors) < 1 {
		t.Errorf("expected at least 1 AK5 error code, got %d", len(ts1.Errors))
	}

	// Check AK3 segment notes
	if len(ts1.SegmentNotes) != 1 {
		t.Fatalf("expected 1 segment note, got %d", len(ts1.SegmentNotes))
	}
	sn := ts1.SegmentNotes[0]
	if sn.SegmentID != "CLP" {
		t.Errorf("expected segment ID 'CLP', got '%s'", sn.SegmentID)
	}
	if sn.SegmentPosition != "4" {
		t.Errorf("expected segment position '4', got '%s'", sn.SegmentPosition)
	}
	if sn.ErrorCode != "5" {
		t.Errorf("expected error code '5', got '%s'", sn.ErrorCode)
	}

	// Check AK4 element notes
	if len(ts1.ElementNotes) != 2 {
		t.Fatalf("expected 2 element notes, got %d", len(ts1.ElementNotes))
	}
	en1 := ts1.ElementNotes[0]
	if en1.ElementPosition != "2" {
		t.Errorf("expected element position '2', got '%s'", en1.ElementPosition)
	}
	if en1.ElementReference != "CLP02" {
		t.Errorf("expected element reference 'CLP02', got '%s'", en1.ElementReference)
	}
	if en1.ErrorCode != "7" {
		t.Errorf("expected error code '7', got '%s'", en1.ErrorCode)
	}
	if en1.BadElement != "INVALID" {
		t.Errorf("expected bad element 'INVALID', got '%s'", en1.BadElement)
	}
	en2 := ts1.ElementNotes[1]
	if en2.BadElement != "999999" {
		t.Errorf("expected bad element '999999', got '%s'", en2.BadElement)
	}

	// Second functional group: accepted
	fg2 := ack.FunctionalGroups[1]
	if fg2.AckCode != "A" {
		t.Errorf("expected AK9 ack code 'A' in group 2, got '%s'", fg2.AckCode)
	}
	if len(fg2.TransactionSets) != 1 {
		t.Fatalf("expected 1 transaction set in group 2, got %d", len(fg2.TransactionSets))
	}
	ts2 := fg2.TransactionSets[0]
	if ts2.AckCode != "A" {
		t.Errorf("expected AK5 ack code 'A', got '%s'", ts2.AckCode)
	}
	if len(ts2.SegmentNotes) != 0 {
		t.Errorf("expected 0 segment notes in accepted TS, got %d", len(ts2.SegmentNotes))
	}

	// Check top-level ack code (from first functional group)
	if ack.AckCode != "R" {
		t.Errorf("expected top-level AckCode 'R', got '%s'", ack.AckCode)
	}
}

func TestX12997Parser_ParseData_Partial(t *testing.T) {
	p := &X12997Parser{}
	_ = p.SetContext(context.Background())

	result, err := p.ParseData(sample997Partial)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var ack X12997Result
	if err := json.Unmarshal([]byte(result), &ack); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(ack.FunctionalGroups) != 1 {
		t.Fatalf("expected 1 functional group, got %d", len(ack.FunctionalGroups))
	}

	fg := ack.FunctionalGroups[0]
	if fg.AckCode != "P" {
		t.Errorf("expected AK9 ack code 'P', got '%s'", fg.AckCode)
	}

	// AK9 has 3 error codes in positions 5-7
	if len(fg.Errors) != 3 {
		t.Errorf("expected 3 AK9 error codes, got %d", len(fg.Errors))
	}
	if fg.Errors[0] != "1" {
		t.Errorf("expected first AK9 error '1', got '%s'", fg.Errors[0])
	}

	if len(fg.TransactionSets) != 1 {
		t.Fatalf("expected 1 transaction set, got %d", len(fg.TransactionSets))
	}

	ts := fg.TransactionSets[0]
	if ts.AckCode != "E" {
		t.Errorf("expected AK5 ack code 'E', got '%s'", ts.AckCode)
	}
	if len(ts.Errors) != 2 {
		t.Errorf("expected 2 AK5 errors, got %d", len(ts.Errors))
	}
}

func TestX12997Parser_ParseData_Empty(t *testing.T) {
	p := &X12997Parser{}
	_ = p.SetContext(context.Background())

	result, err := p.ParseData("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var ack X12997Result
	if err := json.Unmarshal([]byte(result), &ack); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(ack.FunctionalGroups) != 0 {
		t.Errorf("expected 0 functional groups for empty input, got %d", len(ack.FunctionalGroups))
	}
}

func TestX12997Parser_SetContext(t *testing.T) {
	p := &X12997Parser{}
	ctx := context.Background()
	if err := p.SetContext(ctx); err != nil {
		t.Errorf("SetContext should not error: %v", err)
	}
	if p.ctx != ctx {
		t.Error("SetContext did not store context")
	}
}

func TestX12997Parser_Registration(t *testing.T) {
	p, err := InstantiateParser("org.remitt.plugin.parser.X12997Parser")
	if err != nil {
		t.Fatalf("parser not registered: %v", err)
	}
	if p == nil {
		t.Fatal("InstantiateParser returned nil")
	}
	_, ok := p.(*X12997Parser)
	if !ok {
		t.Errorf("expected *X12997Parser, got %T", p)
	}
}
