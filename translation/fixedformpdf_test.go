package translation

import (
	"encoding/xml"
	"io/ioutil"
	"log"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func Test_TranslateFixedFormXmlPDF(t *testing.T) {
	f, err := ioutil.ReadFile("../test/fixedform.xml")
	if err != nil {
		t.Fatalf(err.Error())
	}

	var obj model.FixedFormXml
	err = xml.Unmarshal([]byte(f), &obj)
	if err != nil {
		t.Fatalf(err.Error())
	}

	translator := &TranslateFixedFormPDF{TemplatePath: "../resources/pdf", Benchmark: true}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatalf(err.Error())
	}

	log.Printf("Test_TranslateFixedFormXmlPDF(): Found %d bytes", len(out))

	ioutil.WriteFile("../test/fixedform.pdf", out, 0600)
}
