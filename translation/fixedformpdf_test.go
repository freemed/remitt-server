package translation

import (
	"context"
	"encoding/xml"
	"log"
	"os"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func Test_TranslateFixedFormXmlPDF(t *testing.T) {
	data, err := os.ReadFile("../test/testdata/fixedform_simple.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var obj model.FixedFormXml
	err = xml.Unmarshal(data, &obj)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(obj.Pages) == 0 {
		t.Fatal("expected at least one page in test data")
	}

	translator := &TranslateFixedFormPDF{TemplatePath: "../resources/pdf", Benchmark: true}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	log.Printf("Test_TranslateFixedFormXmlPDF(): Found %d bytes", len(out))

	// Verify PDF magic bytes
	if len(out) < 5 || string(out[:5]) != "%PDF-" {
		t.Errorf("expected PDF output, got %q", string(out[:minInt(len(out), 20)]))
	}
	if len(out) < 100 {
		t.Errorf("expected substantial PDF output (%d bytes), got %d bytes", 100, len(out))
	}

	os.WriteFile("../test/fixedform.pdf", out, 0600)
}

func Test_TranslateFixedFormXmlPDF_SetContext(t *testing.T) {
	translator := &TranslateFixedFormPDF{}
	err := translator.SetContext(context.Background())
	if err != nil {
		t.Errorf("SetContext should not return error: %v", err)
	}
}

func Test_TranslateFixedFormXmlPDF_Resolver(t *testing.T) {
	translator := &TranslateFixedFormPDF{}
	if !translator.Resolver("fixedformxml", "pdf") {
		t.Error("expected Resolver('fixedformxml', 'pdf') to be true")
	}
	if !translator.Resolver("fixedformxml", "*") {
		t.Error("expected Resolver('fixedformxml', '*') to be true")
	}
	if translator.Resolver("fixedformxml", "text") {
		t.Error("expected Resolver('fixedformxml', 'text') to be false")
	}
}

func Test_TranslateFixedFormXmlPDF_InvalidType(t *testing.T) {
	translator := &TranslateFixedFormPDF{}
	_, err := translator.Translate("not a FixedFormXml struct")
	if err == nil {
		t.Error("expected error for invalid input type")
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
