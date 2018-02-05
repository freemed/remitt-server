package translation

import (
	"encoding/xml"
	"io/ioutil"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func Test_TranslateX12Xml(t *testing.T) {
	f, err := ioutil.ReadFile("../test/intermediate.xml")
	if err != nil {
		t.Fatalf(err.Error())
	}

	var obj model.X12Xml
	err = xml.Unmarshal([]byte(f), &obj)
	if err != nil {
		t.Fatalf(err.Error())
	}

	translator := &TranslateX12Xml{}
	out, err := translator.Translate(obj)
	if err != nil {
		t.Fatalf(err.Error())
	}
	ioutil.WriteFile("../test/out.x12", out, 0600)
}
