package translation

import (
	"context"
	"encoding/xml"
	"log"
	"os"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func Test_TranslateFixedFormXML(t *testing.T) {
	data, err := os.ReadFile("../test/testdata/fixedform_simple.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var obj model.FixedFormXml
	err = xml.Unmarshal(data, &obj)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	translator := &TranslateFixedFormXML{}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	log.Printf("Test_TranslateFixedFormXML(): Found %d bytes", len(out))

	// Verify output contains key content
	outStr := string(out)
	if !containsText(outStr, "REMITT TEST FORM") {
		t.Error("expected 'REMITT TEST FORM' in output")
	}
	if len(outStr) == 0 {
		t.Error("expected non-empty output")
	}

	// Write output for manual inspection
	os.WriteFile("../test/fixedform.txt", out, 0600)
}

func Test_TranslateFixedFormXML_SetContext(t *testing.T) {
	translator := &TranslateFixedFormXML{}
	err := translator.SetContext(context.Background())
	if err != nil {
		t.Errorf("SetContext should not return error: %v", err)
	}
}

func Test_TranslateFixedFormXML_Resolver(t *testing.T) {
	translator := &TranslateFixedFormXML{}
	if !translator.Resolver("fixedformxml", "text") {
		t.Error("expected Resolver('fixedformxml', 'text') to be true")
	}
	if !translator.Resolver("fixedformxml", "*") {
		t.Error("expected Resolver('fixedformxml', '*') to be true")
	}
	if translator.Resolver("fixedformxml", "pdf") {
		t.Error("expected Resolver('fixedformxml', 'pdf') to be false")
	}
}

func Test_TranslateFixedFormXML_InvalidType(t *testing.T) {
	translator := &TranslateFixedFormXML{}
	_, err := translator.Translate("not a FixedFormXml struct")
	if err == nil {
		t.Error("expected error for invalid input type")
	}
}

func Test_TranslateFixedFormXML_MultiPage(t *testing.T) {
	data, err := os.ReadFile("../test/testdata/fixedform_simple.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var obj model.FixedFormXml
	err = xml.Unmarshal(data, &obj)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(obj.Pages) < 2 {
		t.Fatal("test data should have at least 2 pages")
	}

	translator := &TranslateFixedFormXML{}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	outStr := string(out)
	// Verify both pages are rendered (content is truncated to element length)
	if !containsText(outStr, "REMITT TEST FORM") {
		t.Error("expected 'REMITT TEST FORM' in multi-page output")
	}
	if !containsText(outStr, "TEST MEDICAL GROUP") {
		t.Error("expected 'TEST MEDICAL GROUP' in multi-page output")
	}
}

func containsText(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
