package translation

import (
	"encoding/xml"
	"log"
	"os"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func Test_TranslateFixedFormXML(t *testing.T) {
	f, err := os.ReadFile("../test/fixedform.xml")
	if err != nil {
		t.Fatal(err.Error())
	}

	var obj model.FixedFormXml
	err = xml.Unmarshal([]byte(f), &obj)
	if err != nil {
		t.Fatal(err.Error())
	}

	translator := &TranslateFixedFormXML{}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatal(err.Error())
	}

	log.Printf("Test_TranslateFixedFormXmlXML(): Found %d bytes", len(out))

	os.WriteFile("../test/fixedform.txt", out, 0600)
}
