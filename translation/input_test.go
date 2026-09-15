package translation

// input_test.go pins the translator input contract (input.go): a Translator is
// handed the render stage's bytes, exactly as the Java pipeline handed every
// plugin the previous stage's byte[] (PluginInterface.java:41 ->
// TranslationProcessorThread.java:80-83), and the typed model stays accepted.
//
// The bytes used here are the real documents: test/testdata/x12_intermediate.xml
// is a rendered x12xml document (root <render>, the shape 4010_837p.xsl and
// 5010_837p.xsl emit) and test/testdata/fixedform_simple.xml is a fixed-form one
// (root <fixedform>, what cms1500.xsl and statement.xsl emit).

import (
	"encoding/xml"
	"os"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/model"
)

// xmlUnmarshal is encoding/xml's decoder, spelled out here so the test decodes
// the fixture exactly the way the pipeline's input contract does.
func xmlUnmarshal(b []byte, v any) error {
	return xml.Unmarshal(b, v)
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// TestTranslate_AcceptsRenderOutputBytes is the regression for the stage that
// could not run: executeJob hands the render stage's []byte to Translate, and
// the bytes path must produce exactly what the typed-model path produces.
func TestTranslate_AcceptsRenderOutputBytes(t *testing.T) {
	x12Bytes := readFixture(t, "../test/testdata/x12_intermediate.xml")
	fixedBytes := readFixture(t, "../test/testdata/fixedform_simple.xml")

	// x12xml: bytes vs the typed model.
	x12 := &TranslateX12Xml{}
	fromBytes, err := x12.Translate(x12Bytes)
	if err != nil {
		t.Fatalf("TranslateX12Xml.Translate(render output bytes) = %v; the pipeline hands bytes", err)
	}
	if len(fromBytes) == 0 {
		t.Fatal("TranslateX12Xml.Translate(bytes) produced no output")
	}
	x12Typed := &TranslateX12Xml{}
	var typedX12 model.X12Xml
	if err := xmlUnmarshal(x12Bytes, &typedX12); err != nil {
		t.Fatalf("decode the fixture into model.X12Xml: %v", err)
	}
	fromModel, err := x12Typed.Translate(typedX12)
	if err != nil {
		t.Fatalf("TranslateX12Xml.Translate(model.X12Xml) = %v", err)
	}
	if string(fromBytes) != string(fromModel) {
		t.Errorf("the bytes path and the typed-model path disagree (%d vs %d bytes)", len(fromBytes), len(fromModel))
	}
	if !strings.Contains(string(fromBytes), "ISA*") {
		t.Errorf("the x12 output does not look like X12: %q", firstN(fromBytes, 80))
	}

	// fixedformxml: bytes vs the typed model.
	ff := &TranslateFixedFormXML{}
	ffFromBytes, err := ff.Translate(fixedBytes)
	if err != nil {
		t.Fatalf("TranslateFixedFormXML.Translate(render output bytes) = %v", err)
	}
	var typedFF model.FixedFormXml
	if err := xmlUnmarshal(fixedBytes, &typedFF); err != nil {
		t.Fatalf("decode the fixture into model.FixedFormXml: %v", err)
	}
	ffFromModel, err := (&TranslateFixedFormXML{}).Translate(typedFF)
	if err != nil {
		t.Fatalf("TranslateFixedFormXML.Translate(model.FixedFormXml) = %v", err)
	}
	if string(ffFromBytes) != string(ffFromModel) {
		t.Errorf("the bytes path and the typed-model path disagree (%d vs %d bytes)", len(ffFromBytes), len(ffFromModel))
	}

	// fixedformpdf: bytes must produce a real PDF (the document embeds a
	// creation date, so a byte comparison between two calls would be flaky).
	pdf, err := (&TranslateFixedFormPDF{TemplatePath: "../resources/pdf"}).Translate(fixedBytes)
	if err != nil {
		t.Fatalf("TranslateFixedFormPDF.Translate(render output bytes) = %v", err)
	}
	if len(pdf) < 100 || string(pdf[:5]) != "%PDF-" {
		t.Errorf("expected a PDF from the bytes path, got %d bytes starting %q", len(pdf), firstN(pdf, 20))
	}

	// A pointer to the model is accepted too.
	if _, err := (&TranslateX12Xml{}).Translate(&typedX12); err != nil {
		t.Errorf("TranslateX12Xml.Translate(*model.X12Xml) = %v", err)
	}
	if _, err := (&TranslateFixedFormXML{}).Translate(&typedFF); err != nil {
		t.Errorf("TranslateFixedFormXML.Translate(*model.FixedFormXml) = %v", err)
	}
}

// TestTranslate_RejectsWhatItCannotConsume keeps the contract explicit: a
// document that is not the model's XML and a type that is neither the model nor
// bytes are both errors, and each plugin reports under its own name.
func TestTranslate_RejectsWhatItCannotConsume(t *testing.T) {
	cases := []struct {
		name   string
		plugin Translator
		input  any
		want   string
	}{
		{"x12xml, wrong root", &TranslateX12Xml{}, []byte("<fixedform><page/></fixedform>"), "x12xml: translate: parse render output as <render>"},
		{"x12xml, not xml at all", &TranslateX12Xml{}, []byte("not xml"), "x12xml: translate: parse render output as <render>"},
		{"x12xml, unsupported type", &TranslateX12Xml{}, 42, "x12xml: translate: invalid datatype presented"},
		{"x12xml, nil pointer", &TranslateX12Xml{}, (*model.X12Xml)(nil), "x12xml: translate: nil *model.X12Xml input"},
		{"fixedformxml, wrong root", &TranslateFixedFormXML{}, []byte("<render><x12format/></render>"), "fixedformxml: translate: parse render output as <fixedform>"},
		{"fixedformxml, unsupported type", &TranslateFixedFormXML{}, 42, "fixedformxml: translate: render: invalid datatype presented"},
		{"fixedformxml, nil pointer", &TranslateFixedFormXML{}, (*model.FixedFormXml)(nil), "fixedformxml: translate: nil *model.FixedFormXml input"},
		{"fixedformpdf, wrong root", &TranslateFixedFormPDF{}, []byte("<render><x12format/></render>"), "fixedformpdf: translate: parse render output as <fixedform>"},
		{"fixedformpdf, unsupported type", &TranslateFixedFormPDF{}, 42, "fixedformpdf: translate: invalid datatype presented"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := c.plugin.Translate(c.input)
			if err == nil {
				t.Fatalf("Translate(%T) = %d bytes with a nil error", c.input, len(out))
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

func firstN(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
