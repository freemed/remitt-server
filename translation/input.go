package translation

// input.go is the translator input contract: what a Translator is handed, and
// how a raw render output becomes the typed model the plugin works on.
//
// THE CONTRACT, AND WHY IT LIVES HERE
//
// The Java pipeline had ONE entry point for all three stages -
// PluginInterface.render(Integer jobId, byte[] input, String option)
// (../remitt/src/main/java/org/remitt/prototype/PluginInterface.java:41) - and
// the translation stage called it with the PREVIOUS STAGE'S BYTES
// (TranslationProcessorThread.java:80-83). Each plugin then parsed those bytes
// into whatever it needed (XStream), so "the translator parses its own input"
// is the original behaviour, not an invention.
//
// The Go port kept the typed models (model.X12Xml for the x12 family,
// model.FixedFormXml for the fixed-form family) - the render stage's stylesheets
// emit exactly those documents, root <render> for x12xml and root <fixedform>
// for fixed-form (resources/xsl/*.xsl; model/x12xml.go:8,
// model/fixedformxml.go:8) - but jobqueue's executeJob hands the plugin the
// render stage's []byte, so until these decoders existed every shipped
// stylesheet failed at the first translator with
//
//	executejob: translate: x12xml: translate: invalid datatype presented
//
// The decoders below are that parse, in the place Java did it. Both inputs are
// accepted:
//
//   - the typed model (or a pointer to it), which is how the plugins' own tests
//     and any caller that already built the model call them;
//   - []byte / string, the render stage's document, decoded with encoding/xml.
//
// Anything else is still the datatype error the plugin always reported, so the
// contract stays explicit rather than silently treating an unknown type as
// empty input.

import (
	"encoding/xml"
	"fmt"

	"github.com/freemed/remitt-server/model"
)

const (
	// x12XmlRoot / fixedFormXmlRoot are the XML roots the typed models declare
	// (model/x12xml.go:8 XMLName xml.Name `xml:"render"`,
	// model/fixedformxml.go:8 `xml:"fixedform"`), used only to make a parse
	// failure name what was expected.
	x12XmlRoot       = "render"
	fixedFormXmlRoot = "fixedform"
)

// x12XmlFromInput returns the X12 model for TranslateX12Xml's input.
func x12XmlFromInput(source any) (model.X12Xml, error) {
	switch v := source.(type) {
	case model.X12Xml:
		return v, nil
	case *model.X12Xml:
		if v == nil {
			return model.X12Xml{}, fmt.Errorf("x12xml: translate: nil *model.X12Xml input")
		}
		return *v, nil
	case []byte:
		var out model.X12Xml
		if err := xml.Unmarshal(v, &out); err != nil {
			return model.X12Xml{}, fmt.Errorf("x12xml: translate: parse render output as <%s>: %w", x12XmlRoot, err)
		}
		return out, nil
	case string:
		var out model.X12Xml
		if err := xml.Unmarshal([]byte(v), &out); err != nil {
			return model.X12Xml{}, fmt.Errorf("x12xml: translate: parse render output as <%s>: %w", x12XmlRoot, err)
		}
		return out, nil
	default:
		return model.X12Xml{}, fmt.Errorf("x12xml: translate: invalid datatype presented")
	}
}

// fixedFormTranslator carries the two strings each fixed-form plugin reports
// under: its name (for the decode and nil-input messages) and the exact
// datatype error it has always used, so neither plugin's message changes.
type fixedFormTranslator struct {
	name       string
	invalidTyp string
}

var (
	fixedFormXmlPlugin = fixedFormTranslator{
		name:       "fixedformxml",
		invalidTyp: "fixedformxml: translate: render: invalid datatype presented",
	}
	fixedFormPdfPlugin = fixedFormTranslator{
		name:       "fixedformpdf",
		invalidTyp: "fixedformpdf: translate: invalid datatype presented",
	}
)

// fixedFormXmlFromInput returns the fixed-form model for one of the two
// fixed-form translators, which consume the same document.
func fixedFormXmlFromInput(source any, p fixedFormTranslator) (model.FixedFormXml, error) {
	switch v := source.(type) {
	case model.FixedFormXml:
		return v, nil
	case *model.FixedFormXml:
		if v == nil {
			return model.FixedFormXml{}, fmt.Errorf("%s: translate: nil *model.FixedFormXml input", p.name)
		}
		return *v, nil
	case []byte:
		return decodeFixedFormXml(v, p)
	case string:
		return decodeFixedFormXml([]byte(v), p)
	default:
		return model.FixedFormXml{}, fmt.Errorf("%s", p.invalidTyp)
	}
}

// decodeFixedFormXml decodes the render stage's fixed-form document, which is
// rooted <fixedform> (model/fixedformxml.go:8) - cms1500.xsl and statement.xsl
// both emit it.
func decodeFixedFormXml(b []byte, p fixedFormTranslator) (model.FixedFormXml, error) {
	var out model.FixedFormXml
	if err := xml.Unmarshal(b, &out); err != nil {
		return model.FixedFormXml{}, fmt.Errorf("%s: translate: parse render output as <%s>: %w", p.name, fixedFormXmlRoot, err)
	}
	return out, nil
}
