package translation

import (
	"encoding/xml"
	"os"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func Test_TranslateX12Xml(t *testing.T) {
	f, err := os.ReadFile("../test/intermediate.xml")
	if err != nil {
		t.Fatal(err.Error())
	}

	var obj model.X12Xml
	err = xml.Unmarshal([]byte(f), &obj)
	if err != nil {
		t.Fatal(err.Error())
	}

	translator := &TranslateX12Xml{}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatal(err.Error())
	}
	os.WriteFile("../test/out.x12", out, 0600)
}
