// Package model: X12 intermediate XML parsing and marshalling (x12xml.go).
//
// # Scope
//
// X12Xml is the intermediate document the "x12xml" render plugin turns back into
// an X12 interchange (translation/x12xml.go). These tests cover the decoding and
// encoding contract of the three structs, in memory, with no database and no
// filesystem beyond the tracked fixture.
//
// # What is pinned
//
//   - The tracked fixture test/testdata/x12_intermediate.xml decodes with the
//     delimiter, terminator, segment order and element values the renderer
//     depends on. Note the fixture's ISA elements carry significant trailing and
//     interior spaces: those are data, not formatting.
//   - Element content is declared `,innerxml`, so it is the raw inner XML:
//     whitespace is preserved verbatim and entity references are NOT decoded
//     (&amp; stays the five bytes "&amp;"). Consumers that concatenate
//     Content.Content into a non-XML output (translation/x12xml.go:90) therefore
//     receive escaped text, not the final value.
//     TestX12ElementContentKeepsRawXML.
//   - The optional element children (hl, counter, resetcounter, segmentcount) and
//     content attributes (text, fixedlength, zeroprepend) are what drive the
//     renderer's counter and padding logic, so their tag mapping is pinned here.
//   - XMLName roots the document: a <fixedform> document cannot decode into
//     X12Xml.
//
// # Documented defect
//
// Marshalling an X12Element/X12Xml does not round-trip: encoding/xml does not
// honour `omitempty` on anonymous struct fields, and innerxml fields that were
// never present emit empty elements with defaulted attributes. See
// TestX12XmlMarshalRoundTripIsNotIdempotent. Nothing in this repository marshals
// model.X12Xml today (only soap/soap.go marshals its own types), so this is a
// latent fidelity issue rather than a live one.
package model

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestX12XmlFixtureParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(modelRepoRoot(t), "test", "testdata", "x12_intermediate.xml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var x X12Xml
	if err := xml.Unmarshal(data, &x); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}

	if x.XMLName.Local != "render" {
		t.Errorf("XMLName.Local = %q, want %q", x.XMLName.Local, "render")
	}
	if x.X12Format.Delimiter != "*" {
		t.Errorf("delimiter = %q, want %q", x.X12Format.Delimiter, "*")
	}
	if x.X12Format.EndOfLine != "~" {
		t.Errorf("endofline = %q, want %q", x.X12Format.EndOfLine, "~")
	}

	wantIDs := []string{"ISA", "GS", "ST", "BPR", "TRN", "N1", "N1", "LX", "CLP", "NM1", "SE", "GE", "IEA"}
	if len(x.Segments) != len(wantIDs) {
		t.Fatalf("segments = %d, want %d", len(x.Segments), len(wantIDs))
	}
	for i, want := range wantIDs {
		if got := x.Segments[i].SegmentId; got != want {
			t.Errorf("segment %d sid = %q, want %q", i, got, want)
		}
	}

	// Comments are per segment and are optional in general.
	if got := x.Segments[0].Comment; got != "Interchange Control Header" {
		t.Errorf("ISA comment = %q", got)
	}
	if got := x.Segments[1].Comment; got != "Functional Group Header" {
		t.Errorf("GS comment = %q", got)
	}

	isa := x.Segments[0]
	if len(isa.Elements) != 16 {
		t.Fatalf("ISA elements = %d, want 16", len(isa.Elements))
	}

	// Element content is verbatim: the fixture's fixed-width ISA fields keep
	// their padding spaces, and an element may legitimately be empty.
	wantISA := []string{
		"00", "          ", "00", "          ", "ZZ",
		"REMITT          ", "ZZ", "RECEIVER        ",
		"240810", "1200", "U", "00401", "000000001", "0", "P", ":",
	}
	for i, want := range wantISA {
		if got := isa.Elements[i].Content.Content; got != want {
			t.Errorf("ISA element %d content = %q, want %q", i, got, want)
		}
	}

	// The empty BPR element (element 5) decodes to the empty string.
	bpr := x.Segments[3]
	if len(bpr.Elements) != 12 {
		t.Fatalf("BPR elements = %d, want 12", len(bpr.Elements))
	}
	if got := bpr.Elements[4].Content.Content; got != "" {
		t.Errorf("BPR element 5 = %q, want the empty string", got)
	}
	if got := bpr.Elements[1].Content.Content; got != "1250.00" {
		t.Errorf("BPR element 2 = %q, want 1250.00", got)
	}

	// The SE segment's first element is the segment-count placeholder, written
	// as `<counter name="segcount">` in this fixture (the XSL generators emit
	// `<segmentcount>*</segmentcount>` instead - see resources/xsl/5010_837p.xsl:389).
	se := x.Segments[10]
	if se.Elements[0].Counter.Name != "segcount" {
		t.Errorf("SE element 1 counter name = %q, want segcount", se.Elements[0].Counter.Name)
	}
	if se.Elements[0].Counter.Counter != "" {
		t.Errorf("SE element 1 counter value = %q, want the empty string (the <name> child is not matched)", se.Elements[0].Counter.Counter)
	}
	if got := se.Elements[1].Content.Content; got != "0001" {
		t.Errorf("SE element 2 = %q, want 0001", got)
	}

	// No fixture element uses the remaining renderer children or content
	// attributes; the counter placeholder above is the only exception.
	for si, seg := range x.Segments {
		for ei, el := range seg.Elements {
			if el.Hl != "" || el.SegmentCount != "" {
				t.Errorf("segment %d element %d unexpectedly uses a renderer child: %+v", si, ei, el)
			}
			if el.ResetCounter.Name != "" {
				t.Errorf("segment %d element %d unexpectedly uses resetcounter", si, ei)
			}
			if el.Counter.Name != "" && !(si == 10 && ei == 0) {
				t.Errorf("segment %d element %d unexpectedly uses a counter: %+v", si, ei, el.Counter)
			}
			if el.Content.Text != "" || el.Content.FixedLength != 0 || el.Content.ZeroPrepend != 0 {
				t.Errorf("segment %d element %d unexpectedly uses content attributes: %+v", si, ei, el.Content)
			}
		}
	}
}

func TestX12ElementContentKeepsRawXML(t *testing.T) {
	// Documented behaviour: Content.Content is `xml:",innerxml"`, so it is the
	// raw inner XML of <content> - whitespace included and entities unresolved.
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "pretty_printed_content_is_not_trimmed",
			doc:  "<render><x12segment sid=\"A\"><element><content>\n  X\n</content></element></x12segment></render>",
			want: "\n  X\n",
		},
		{
			name: "formatting_spaces_inside_a_single_line_are_data",
			doc:  "<render><x12segment sid=\"A\"><element><content>          </content></element></x12segment></render>",
			want: "          ",
		},
		{
			name: "empty_element",
			doc:  "<render><x12segment sid=\"A\"><element><content></content></element></x12segment></render>",
			want: "",
		},
		{
			name: "self_closing_element",
			doc:  "<render><x12segment sid=\"A\"><element><content/></element></x12segment></render>",
			want: "",
		},
		{
			name: "entities_are_not_decoded",
			doc:  "<render><x12segment sid=\"A\"><element><content>a&amp;b&lt;c</content></element></x12segment></render>",
			want: "a&amp;b&lt;c",
		},
		{
			name: "cdata_markers_are_kept",
			doc:  "<render><x12segment sid=\"A\"><element><content><![CDATA[a&b]]></content></element></x12segment></render>",
			// innerxml is the raw inner XML, so a CDATA section is handed back with
			// its markers and never unwrapped into its text value.
			want: "<![CDATA[a&b]]>",
		},
		{
			name: "nested_markup_is_kept_verbatim",
			doc:  "<render><x12segment sid=\"A\"><element><content><b>x</b></content></element></x12segment></render>",
			want: "<b>x</b>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var x X12Xml
			if err := xml.Unmarshal([]byte(tc.doc), &x); err != nil {
				t.Fatalf("xml.Unmarshal: %v", err)
			}
			if len(x.Segments) != 1 || len(x.Segments[0].Elements) != 1 {
				t.Fatalf("unexpected shape: %d segments", len(x.Segments))
			}
			got := x.Segments[0].Elements[0].Content.Content
			if got != tc.want {
				t.Errorf("content = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestX12ElementOptionalChildrenAndAttributes(t *testing.T) {
	doc := `<render>
	  <x12format><delimiter>*</delimiter><endofline>~</endofline></x12format>
	  <x12segment sid="HL">
	    <comment>Hierarchical Level</comment>
	    <element><hl>2</hl><content text="fallback"/></element>
	    <element><counter name="HLCOUNT">9</counter></element>
	    <element><resetcounter name="HLCOUNT"/><content>reset</content></element>
	    <element><segmentcount>x</segmentcount></element>
	    <element><content text="T" fixedlength="8" zeroprepend="3">42</content></element>
	  </x12segment>
	</render>`

	var x X12Xml
	if err := xml.Unmarshal([]byte(doc), &x); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if len(x.Segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(x.Segments))
	}
	seg := x.Segments[0]
	if seg.SegmentId != "HL" || seg.Comment != "Hierarchical Level" {
		t.Errorf("segment header = %q/%q", seg.SegmentId, seg.Comment)
	}
	if len(seg.Elements) != 5 {
		t.Fatalf("elements = %d, want 5", len(seg.Elements))
	}

	els := seg.Elements
	if els[0].Hl != "2" {
		t.Errorf("element 0 hl = %q, want 2", els[0].Hl)
	}
	if els[0].Content.Text != "fallback" {
		t.Errorf("element 0 text attr = %q, want fallback", els[0].Content.Text)
	}
	// Documented defect: the counter child is an anonymous struct tagged
	// `xml:"counter"` whose INNER field is also tagged `xml:"counter"`
	// (x12xml.go:25-28), so the element's own character data has nowhere to go.
	// `<counter name="HLCOUNT">9</counter>` therefore yields only the name; a
	// value could only arrive from a nested `<counter><counter>9</counter></counter>`.
	// No producer emits that shape - the XSL generators emit
	// `<counter name="..."/>` (resources/xsl/5010_837p.xsl:1542) - so
	// Counter.Counter is dead, and the renderer never reads it either
	// (translation/x12xml.go:69-78 uses only Counter.Name).
	if els[1].Counter.Name != "HLCOUNT" {
		t.Errorf("element 1 counter name = %q, want HLCOUNT", els[1].Counter.Name)
	}
	if els[1].Counter.Counter != "" {
		t.Errorf("element 1 counter value = %q; the inner <counter> element did not exist, so it must stay empty", els[1].Counter.Counter)
	}
	if els[2].ResetCounter.Name != "HLCOUNT" {
		t.Errorf("element 2 resetcounter name = %q, want HLCOUNT", els[2].ResetCounter.Name)
	}
	if els[3].SegmentCount != "x" {
		t.Errorf("element 3 segmentcount = %q, want x", els[3].SegmentCount)
	}
	if els[4].Content.Content != "42" || els[4].Content.FixedLength != 8 || els[4].Content.ZeroPrepend != 3 {
		t.Errorf("element 4 content = %+v, want 42 with fixedlength 8 and zeroprepend 3", els[4].Content)
	}
}

func TestX12ElementDefaultsAreZero(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{name: "self_closing_element", doc: `<render><x12segment sid="A"><element/></x12segment></render>`},
		{name: "empty_element", doc: `<render><x12segment sid="A"><element></element></x12segment></render>`},
		{name: "content_without_attributes", doc: `<render><x12segment sid="A"><element><content/></element></x12segment></render>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var x X12Xml
			if err := xml.Unmarshal([]byte(tc.doc), &x); err != nil {
				t.Fatalf("xml.Unmarshal: %v", err)
			}
			el := x.Segments[0].Elements[0]
			if el.Hl != "" || el.SegmentCount != "" || el.Counter.Name != "" || el.Counter.Counter != "" ||
				el.ResetCounter.Name != "" || el.Content.Content != "" || el.Content.Text != "" ||
				el.Content.FixedLength != 0 || el.Content.ZeroPrepend != 0 {
				t.Errorf("element is not zero valued: %+v", el)
			}
		})
	}
}

func TestX12XmlRejectsOtherRootElements(t *testing.T) {
	for _, doc := range []string{
		`<fixedform><page/></fixedform>`,
		`<renderx><x12format><delimiter>*</delimiter></x12format></renderx>`,
		``,
	} {
		var x X12Xml
		err := xml.Unmarshal([]byte(doc), &x)
		if err == nil {
			t.Errorf("expected an error decoding %q, got %+v", doc, x)
			continue
		}
		if len(x.Segments) != 0 {
			t.Errorf("a rejected document must not populate segments: %+v", x.Segments)
		}
	}
}

func TestX12XmlSegmentAndElementSlices(t *testing.T) {
	t.Run("no_segments", func(t *testing.T) {
		var x X12Xml
		if err := xml.Unmarshal([]byte(`<render><x12format><delimiter>|</delimiter><endofline>!</endofline></x12format></render>`), &x); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		if len(x.Segments) != 0 {
			t.Errorf("segments = %d, want 0", len(x.Segments))
		}
		if x.X12Format.Delimiter != "|" || x.X12Format.EndOfLine != "!" {
			t.Errorf("format = %+v; any single character is accepted unvalidated", x.X12Format)
		}
	})

	t.Run("segment_without_elements", func(t *testing.T) {
		var x X12Xml
		if err := xml.Unmarshal([]byte(`<render><x12segment sid="BHT"/></render>`), &x); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		if len(x.Segments) != 1 || len(x.Segments[0].Elements) != 0 {
			t.Fatalf("unexpected shape: %+v", x.Segments)
		}
		if x.Segments[0].SegmentId != "BHT" {
			t.Errorf("sid = %q, want BHT", x.Segments[0].SegmentId)
		}
	})

	t.Run("unknown_child_elements_are_ignored", func(t *testing.T) {
		var x X12Xml
		doc := `<render><x12segment sid="A"><unknown>x</unknown><element><content>v</content></element></x12segment></render>`
		if err := xml.Unmarshal([]byte(doc), &x); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		if len(x.Segments[0].Elements) != 1 || x.Segments[0].Elements[0].Content.Content != "v" {
			t.Fatalf("unexpected shape: %+v", x.Segments[0])
		}
	})

	t.Run("sid_attribute_missing", func(t *testing.T) {
		var x X12Xml
		if err := xml.Unmarshal([]byte(`<render><x12segment><element/></x12segment></render>`), &x); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		if x.Segments[0].SegmentId != "" {
			t.Errorf("sid = %q, want the empty string (the renderer would emit a bare delimiter)", x.Segments[0].SegmentId)
		}
	})
}

func TestX12XmlMarshalRoundTripIsNotIdempotent(t *testing.T) {
	// Documented defect: encoding/xml ignores `omitempty` on the anonymous
	// structs declared for counter/resetcounter/content, and the fields that are
	// not strings (fixedlength, zeroprepend) always emit their zero attribute
	// values. Marshalling therefore produces elements and attributes that were
	// not in the input. Re-parsing is harmless - the meaningful values survive -
	// but a marshal/unmarshal cycle is not byte-stable, so anything that used
	// model.X12Xml as a serialisation format would emit different XML each time.
	const doc = `<render><x12format><delimiter>*</delimiter><endofline>~</endofline></x12format><x12segment sid="ISA"><comment>c</comment><element><content>00</content></element><element><content>          </content></element></x12segment></render>`

	var x X12Xml
	if err := xml.Unmarshal([]byte(doc), &x); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}

	out, err := xml.Marshal(x)
	if err != nil {
		t.Fatalf("xml.Marshal: %v", err)
	}
	got := string(out)

	if got == doc {
		t.Fatal("the marshalled document is byte-identical to the input; update this test if omitempty was fixed")
	}
	for _, artifact := range []string{
		"<hl></hl>",
		`<counter name=""><counter></counter></counter>`,
		`<resetcounter name=""></resetcounter>`,
		`text=""`,
		`fixedlength="0"`,
		`zeroprepend="0"`,
	} {
		if !strings.Contains(got, artifact) {
			t.Errorf("expected the synthesised artifact %s in:\n%s", artifact, got)
		}
	}

	// The data that matters survives a re-parse.
	var back X12Xml
	if err := xml.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if back.X12Format.Delimiter != x.X12Format.Delimiter || back.X12Format.EndOfLine != x.X12Format.EndOfLine {
		t.Errorf("format changed: %+v -> %+v", x.X12Format, back.X12Format)
	}
	if len(back.Segments) != len(x.Segments) {
		t.Fatalf("segments = %d, want %d", len(back.Segments), len(x.Segments))
	}
	for i := range x.Segments {
		if back.Segments[i].SegmentId != x.Segments[i].SegmentId {
			t.Errorf("segment %d sid changed", i)
		}
		if len(back.Segments[i].Elements) != len(x.Segments[i].Elements) {
			t.Fatalf("segment %d element count changed", i)
		}
		for j := range x.Segments[i].Elements {
			if got, want := back.Segments[i].Elements[j].Content.Content, x.Segments[i].Elements[j].Content.Content; got != want {
				t.Errorf("segment %d element %d content = %q, want %q", i, j, got, want)
			}
		}
	}
}

func TestX12XmlMarshalPreservesRendererChildren(t *testing.T) {
	// The children and attributes the renderer reads survive a marshal/re-parse
	// cycle, even though extra artifacts appear alongside them.
	in := X12Xml{}
	in.X12Format.Delimiter = "*"
	in.X12Format.EndOfLine = "~"
	in.Segments = []X12Segment{{
		SegmentId: "HL",
		Comment:   "Hierarchical Level",
		Elements: []X12Element{
			{Hl: "2"},
			{Counter: elementCounter("HL01", "7")},
			{ResetCounter: elementResetCounter("HL01")},
			{SegmentCount: "3"},
			{Content: elementContent("42", "T", 8, 3)},
		},
	}}

	out, err := xml.Marshal(in)
	if err != nil {
		t.Fatalf("xml.Marshal: %v", err)
	}
	var back X12Xml
	if err := xml.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	got := back.Segments[0].Elements
	if got[0].Hl != "2" {
		t.Errorf("hl = %q", got[0].Hl)
	}
	if got[1].Counter.Name != "HL01" {
		t.Errorf("counter name = %q", got[1].Counter.Name)
	}
	if got[2].ResetCounter.Name != "HL01" {
		t.Errorf("resetcounter name = %q", got[2].ResetCounter.Name)
	}
	if got[3].SegmentCount != "3" {
		t.Errorf("segmentcount = %q", got[3].SegmentCount)
	}
	if got[4].Content.Content != "42" || got[4].Content.Text != "T" || got[4].Content.FixedLength != 8 || got[4].Content.ZeroPrepend != 3 {
		t.Errorf("content = %+v", got[4].Content)
	}
}

// elementCounter builds an X12Element with the counter child populated. The
// child is an anonymous struct, so it must be filled field by field.
func elementCounter(name, value string) (c struct {
	Counter string `xml:"counter"`
	Name    string `xml:"name,attr"`
}) {
	c.Counter = value
	c.Name = name
	return c
}

func elementResetCounter(name string) (r struct {
	Name string `xml:"name,attr"`
}) {
	r.Name = name
	return r
}

func elementContent(content, text string, fixedLength, zeroPrepend int) (c struct {
	Content     string `xml:",innerxml"`
	Text        string `xml:"text,attr"`
	FixedLength int    `xml:"fixedlength,attr"`
	ZeroPrepend int    `xml:"zeroprepend,attr"`
}) {
	c.Content = content
	c.Text = text
	c.FixedLength = fixedLength
	c.ZeroPrepend = zeroPrepend
	return c
}
