package translation

import (
	"context"
	"encoding/xml"
	"os"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func Test_TranslateX12Xml(t *testing.T) {
	data, err := os.ReadFile("../test/testdata/x12_intermediate.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var obj model.X12Xml
	err = xml.Unmarshal(data, &obj)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	translator := &TranslateX12Xml{}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	// Verify output is non-empty
	if len(out) == 0 {
		t.Error("expected non-empty X12 EDI output")
	}

	// Verify key segments are present
	outStr := string(out)
	for _, seg := range []string{"ISA*", "GS*", "ST*835*", "BPR*", "TRN*", "CLP*", "SE*", "GE*", "IEA*"} {
		if !containsString(outStr, seg) {
			t.Errorf("expected segment %q in output", seg)
		}
	}

	// Write output for manual inspection
	os.WriteFile("../test/out.x12", out, 0600)
}

func Test_TranslateX12Xml_SetContext(t *testing.T) {
	translator := &TranslateX12Xml{}
	err := translator.SetContext(context.Background())
	if err != nil {
		t.Errorf("SetContext should not return error: %v", err)
	}
}

func Test_TranslateX12Xml_Resolver(t *testing.T) {
	translator := &TranslateX12Xml{}
	if !translator.Resolver("x12xml", "x12") {
		t.Error("expected Resolver('x12xml', 'x12') to be true")
	}
	if !translator.Resolver("x12xml", "*") {
		t.Error("expected Resolver('x12xml', '*') to be true")
	}
	if translator.Resolver("x12xml", "pdf") {
		t.Error("expected Resolver('x12xml', 'pdf') to be false")
	}
}

func Test_TranslateX12Xml_InvalidType(t *testing.T) {
	translator := &TranslateX12Xml{}
	_, err := translator.Translate("not an X12Xml struct")
	if err == nil {
		t.Error("expected error for invalid input type")
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
