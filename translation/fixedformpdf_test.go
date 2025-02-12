package translation

import (
	"encoding/xml"
	"log"
	"os"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func Test_TranslateFixedFormXmlPDF(t *testing.T) {
	f, err := os.ReadFile("../test/fixedform.xml")
	if err != nil {
		t.Fatal(err.Error())
	}

	var obj model.FixedFormXml
	err = xml.Unmarshal([]byte(f), &obj)
	if err != nil {
		t.Fatal(err.Error())
	}

	translator := &TranslateFixedFormPDF{TemplatePath: "../resources/pdf", Benchmark: true}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatal(err.Error())
	}

	log.Printf("Test_TranslateFixedFormXmlPDF(): Found %d bytes", len(out))

	os.WriteFile("../test/fixedform.pdf", out, 0600)
}
