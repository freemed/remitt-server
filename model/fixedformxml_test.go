// Package model: fixed-form XML parsing (fixedformxml.go).
//
// # Scope
//
// FixedFormXml is the document the "fixedformxml" render plugin consumes: a page
// list, per-page PDF formatting, and a list of positioned elements. These tests
// cover only the decoding contract - encoding/xml's mapping onto the struct
// tags - with no database, no filesystem beyond the checked-in fixture, and no
// PDF rendering (that is translation/fixedformpdf_test.go).
//
// # What is pinned
//
//   - The tracked fixture test/testdata/fixedform_simple.xml decodes with the
//     page, format and element values the renderer depends on.
//   - Element content is taken VERBATIM and is never trimmed: the chardata of
//     <content> reaches Content with its indentation and newlines intact, so a
//     pretty-printed document carries that whitespace into the fixed-form
//     output. Fixtures must keep <content>...</content> on one line.
//     TestFixedElementContentIsVerbatim.
//   - Entity references in content ARE decoded (this is a chardata field, not
//     innerxml): <content>a&amp;b</content> yields "a&b". Contrast
//     X12Element.Content in x12xml_test.go, which keeps the raw escaped bytes.
//   - The root element type is part of the contract: XMLName rejects any
//     document whose root is not <fixedform>.
//   - FixedElements implements sort.Interface over (Row, Column).
package model

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// modelRepoRoot locates the repository root from this source file, so the
// fixture path does not depend on the process working directory (the same
// approach as validation/x12validator_test.go:101-112).
func modelRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine the test source location")
	}
	root := filepath.Dir(filepath.Dir(file))
	if _, err := os.Stat(filepath.Join(root, "test", "testdata", "fixedform_simple.xml")); err != nil {
		t.Fatalf("fixed-form fixture missing under %s: %v", root, err)
	}
	return root
}

func TestFixedFormXmlFixtureParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(modelRepoRoot(t), "test", "testdata", "fixedform_simple.xml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var ff FixedFormXml
	if err := xml.Unmarshal(data, &ff); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}

	if ff.XMLName.Local != "fixedform" {
		t.Errorf("XMLName.Local = %q, want %q", ff.XMLName.Local, "fixedform")
	}
	if len(ff.Pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(ff.Pages))
	}

	// Page 1 formatting block.
	p0 := ff.Pages[0]
	if p0.Format.PageLength != 66 {
		t.Errorf("page 1 pagelength = %d, want 66", p0.Format.PageLength)
	}
	if p0.Format.Pdf.Template != "blank" {
		t.Errorf("page 1 pdf template = %q, want %q", p0.Format.Pdf.Template, "blank")
	}
	if p0.Format.Pdf.Page != 1 {
		t.Errorf("page 1 pdf page = %d, want 1", p0.Format.Pdf.Page)
	}
	if p0.Format.Pdf.Font.Name != "Courier" || p0.Format.Pdf.Font.Size != 10 {
		t.Errorf("page 1 font = %q/%v, want Courier/10", p0.Format.Pdf.Font.Name, p0.Format.Pdf.Font.Size)
	}
	if p0.Format.Pdf.Scaling.Vertical != 12 || p0.Format.Pdf.Scaling.Horizontal != 6.5 {
		t.Errorf("page 1 scaling = %v/%v, want 12/6.5", p0.Format.Pdf.Scaling.Vertical, p0.Format.Pdf.Scaling.Horizontal)
	}
	if p0.Format.Pdf.Offset.Vertical != 72 || p0.Format.Pdf.Offset.Horizontal != 72 {
		t.Errorf("page 1 offset = %v/%v, want 72/72", p0.Format.Pdf.Offset.Vertical, p0.Format.Pdf.Offset.Horizontal)
	}
	if len(p0.Elements) != 17 {
		t.Errorf("page 1 elements = %d, want 17", len(p0.Elements))
	}
	if len(ff.Pages[1].Elements) != 9 {
		t.Errorf("page 2 elements = %d, want 9", len(ff.Pages[1].Elements))
	}
	if ff.Pages[1].Format.Pdf.Template != "blank" || ff.Pages[1].Format.PageLength != 66 {
		t.Errorf("page 2 format block did not parse: %+v", ff.Pages[1].Format)
	}

	// Spot-check the elements every renderer test relies on, in document order.
	want := []FixedElement{
		{Row: 5, Column: 30, Length: 15, Content: "REMITT TEST FORM"},
		{Row: 8, Column: 5, Length: 15, Content: "Patient Name:"},
		{Row: 8, Column: 22, Length: 25, Content: "JOHN Q DOE"},
		{Row: 10, Column: 5, Length: 15, Content: "Date of Birth:"},
		{Row: 10, Column: 22, Length: 12, Content: "01/15/1980"},
	}
	for i, w := range want {
		if i >= len(p0.Elements) {
			t.Fatalf("fixture has only %d elements, wanted at least %d", len(p0.Elements), i+1)
		}
		if p0.Elements[i] != w {
			t.Errorf("element %d = %+v, want %+v", i, p0.Elements[i], w)
		}
	}

	// The remaining pinned elements are located by position rather than index,
	// because the document interleaves labels and values.
	byPosition := map[[2]int]FixedElement{}
	seen := map[[2]int]bool{}
	for _, el := range p0.Elements {
		key := [2]int{el.Row, el.Column}
		if seen[key] {
			t.Errorf("fixture has two elements at row %d column %d", el.Row, el.Column)
		}
		seen[key] = true
		byPosition[key] = el
	}
	for _, w := range []FixedElement{
		{Row: 12, Column: 22, Length: 20, Content: "CLM-2024-001234"},
		{Row: 20, Column: 22, Length: 12, Content: "$250.00"},
		{Row: 25, Column: 5, Length: 50, Content: "This is an electronic remittance advice test document."},
		{Row: 27, Column: 5, Length: 50, Content: "Generated by REMITT E2E Test Suite"},
	} {
		got, ok := byPosition[[2]int{w.Row, w.Column}]
		if !ok {
			t.Errorf("no element at row %d column %d", w.Row, w.Column)
			continue
		}
		if got != w {
			t.Errorf("element at row %d column %d = %+v, want %+v", w.Row, w.Column, got, w)
		}
	}

	// The fixture keeps every <content> on one line, which is why no value
	// carries stray whitespace into the renderer.
	for pi, page := range ff.Pages {
		for ei, el := range page.Elements {
			if strings.TrimSpace(el.Content) != el.Content {
				t.Errorf("page %d element %d content has untrimmed whitespace: %q", pi, ei, el.Content)
			}
			if el.Content == "" && el.Row != 0 {
				t.Errorf("page %d element %d has content that decoded empty: %+v", pi, ei, el)
			}
		}
	}
}

func TestFixedElementContentIsVerbatim(t *testing.T) {
	// Documented behaviour: Content is a plain chardata field, so encoding/xml
	// hands back the element's bytes unchanged - indentation, newlines and
	// trailing spaces included. Nothing trims it, and no element that consumes
	// it (translation/fixedformxml.go, translation/fixedformpdf.go) trims it
	// either, so the whitespace ends up in the rendered output.
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "pretty_printed_content_keeps_indentation_and_newlines",
			doc:  "<fixedform><page><element><row>3</row><column>9</column><length>4</length><content>\n   HELLO\n  </content></element></page></fixedform>",
			want: "\n   HELLO\n  ",
		},
		{
			name: "single_leading_and_trailing_space_preserved",
			doc:  "<fixedform><page><element><content> X </content></element></page></fixedform>",
			want: " X ",
		},
		{
			name: "spaces_only_content_is_significant",
			doc:  "<fixedform><page><element><content>          </content></element></page></fixedform>",
			want: "          ",
		},
		{
			name: "tab_characters_preserved",
			doc:  "<fixedform><page><element><content>\tx\ty</content></element></page></fixedform>",
			want: "\tx\ty",
		},
		{
			name: "carriage_returns_are_normalised_by_the_parser",
			doc:  "<fixedform><page><element><content>a\r\nb</content></element></page></fixedform>",
			// XML end-of-line handling (XML 1.0 section 2.11) turns CRLF into a
			// single LF before the decoder sees it, so a CR can never reach
			// Content. Everything else about the bytes is preserved.
			want: "a\nb",
		},
		{
			name: "empty_element",
			doc:  "<fixedform><page><element><content></content></element></page></fixedform>",
			want: "",
		},
		{
			name: "self_closing_element",
			doc:  "<fixedform><page><element><content/></element></page></fixedform>",
			want: "",
		},
		{
			name: "zero_padded_number_is_text_not_a_number",
			doc:  "<fixedform><page><element><content>0007</content></element></page></fixedform>",
			want: "0007",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ff FixedFormXml
			if err := xml.Unmarshal([]byte(tc.doc), &ff); err != nil {
				t.Fatalf("xml.Unmarshal: %v", err)
			}
			if len(ff.Pages) != 1 || len(ff.Pages[0].Elements) != 1 {
				t.Fatalf("unexpected shape: %d pages, %d elements", len(ff.Pages), len(ff.Pages[0].Elements))
			}
			got := ff.Pages[0].Elements[0].Content
			if got != tc.want {
				t.Errorf("content = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFixedElementContentDecodesEntities(t *testing.T) {
	// chardata decoding resolves character and entity references, and a numeric
	// reference is resolved too. This is the opposite of X12Element.Content,
	// which is declared innerxml and therefore keeps escaped text as-is.
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{name: "ampersand", doc: "<content>a&amp;b</content>", want: "a&b"},
		{name: "less_than", doc: "<content>a&lt;b</content>", want: "a<b"},
		{name: "greater_than", doc: "<content>a&gt;b</content>", want: "a>b"},
		{name: "numeric_reference", doc: "<content>&#65;&#x42;</content>", want: "AB"},
		{name: "cdata_section", doc: "<content><![CDATA[a&b]]></content>", want: "a&b"},
		{name: "predefined_quote_entity", doc: "<content>&quot;q&quot;</content>", want: `"q"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := "<fixedform><page><element>" + tc.doc + "</element></page></fixedform>"
			var ff FixedFormXml
			if err := xml.Unmarshal([]byte(doc), &ff); err != nil {
				t.Fatalf("xml.Unmarshal: %v", err)
			}
			got := ff.Pages[0].Elements[0].Content
			if got != tc.want {
				t.Errorf("content = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFixedElementNumericFields(t *testing.T) {
	t.Run("missing_numbers_are_zero", func(t *testing.T) {
		var ff FixedFormXml
		doc := "<fixedform><page><element><content>x</content></element></page></fixedform>"
		if err := xml.Unmarshal([]byte(doc), &ff); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		el := ff.Pages[0].Elements[0]
		if el.Row != 0 || el.Column != 0 || el.Length != 0 {
			t.Errorf("element = %+v, want zeroed positions", el)
		}
	})

	t.Run("negative_and_large_values", func(t *testing.T) {
		var ff FixedFormXml
		doc := "<fixedform><page><element><row>-1</row><column>999</column><length>99999</length><content>x</content></element></page></fixedform>"
		if err := xml.Unmarshal([]byte(doc), &ff); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		el := ff.Pages[0].Elements[0]
		if el.Row != -1 || el.Column != 999 || el.Length != 99999 {
			t.Errorf("element = %+v, want row=-1 column=999 length=99999", el)
		}
	})

	t.Run("non_numeric_row_is_an_error", func(t *testing.T) {
		var ff FixedFormXml
		doc := "<fixedform><page><element><row>abc</row></element></page></fixedform>"
		err := xml.Unmarshal([]byte(doc), &ff)
		if err == nil {
			t.Fatalf("expected an error for a non-numeric row, got %+v", ff.Pages[0].Elements)
		}
		if !strings.Contains(err.Error(), "abc") {
			t.Errorf("error = %v, want it to name the offending value", err)
		}
	})
}

func TestFixedElementBooleanAttributes(t *testing.T) {
	cases := []struct {
		name       string
		attrs      string
		wantOmit   bool
		wantStrip  bool
		wantErrSub string
	}{
		{name: "absent", attrs: "", wantOmit: false, wantStrip: false},
		{name: "true_words", attrs: ` omitpdf="true" periodstrippdf="true"`, wantOmit: true, wantStrip: true},
		{name: "false_words", attrs: ` omitpdf="false" periodstrippdf="false"`, wantOmit: false, wantStrip: false},
		{name: "one_and_zero", attrs: ` omitpdf="1" periodstrippdf="0"`, wantOmit: true, wantStrip: false},
		{name: "uppercase_true", attrs: ` omitpdf="TRUE" periodstrippdf="T"`, wantOmit: true, wantStrip: true},
		{name: "mixed_case_true", attrs: ` omitpdf="True"`, wantOmit: true},
		{name: "independent_flags", attrs: ` omitpdf="true" periodstrippdf="false"`, wantOmit: true, wantStrip: false},
		{name: "garbage_value_is_an_error", attrs: ` omitpdf="yes"`, wantErrSub: "yes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := "<fixedform><page><element" + tc.attrs + "><row>1</row><content>x</content></element></page></fixedform>"
			var ff FixedFormXml
			err := xml.Unmarshal([]byte(doc), &ff)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatal("expected an error for a non-boolean attribute")
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("xml.Unmarshal: %v", err)
			}
			el := ff.Pages[0].Elements[0]
			if el.OmitPdf != tc.wantOmit || el.PeriodStripPdf != tc.wantStrip {
				t.Errorf("element = %+v, want omitpdf=%v periodstrippdf=%v", el, tc.wantOmit, tc.wantStrip)
			}
		})
	}
}

func TestFixedFormXmlRejectsOtherRootElements(t *testing.T) {
	// XMLName is tagged `xml:"fixedform"`, so decoding is rooted: a document for
	// a different renderer cannot be silently accepted (which matters because
	// translation.TranslateFixedFormXML type-asserts on model.FixedFormXml).
	cases := []struct {
		name string
		doc  string
	}{
		{name: "wrong_root", doc: `<notfixedform><page/></notfixedform>`},
		{name: "x12_document", doc: `<render><x12format><delimiter>*</delimiter></x12format></render>`},
		{name: "no_root_element", doc: ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ff FixedFormXml
			err := xml.Unmarshal([]byte(tc.doc), &ff)
			if err == nil {
				t.Fatalf("expected an error decoding %q, got %+v", tc.doc, ff)
			}
			if !strings.Contains(err.Error(), "expected element type") && !strings.Contains(err.Error(), "EOF") {
				t.Errorf("unexpected error text: %v", err)
			}
			if len(ff.Pages) != 0 {
				t.Errorf("a rejected document must not populate pages: %+v", ff.Pages)
			}
		})
	}
}

func TestFixedFormXmlEmptyAndMinimalDocuments(t *testing.T) {
	t.Run("no_pages", func(t *testing.T) {
		var ff FixedFormXml
		if err := xml.Unmarshal([]byte(`<fixedform></fixedform>`), &ff); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		if len(ff.Pages) != 0 {
			t.Errorf("pages = %d, want 0", len(ff.Pages))
		}
	})

	t.Run("page_without_elements", func(t *testing.T) {
		var ff FixedFormXml
		if err := xml.Unmarshal([]byte(`<fixedform><page><format><pagelength>60</pagelength></format></page></fixedform>`), &ff); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		if len(ff.Pages) != 1 {
			t.Fatalf("pages = %d, want 1", len(ff.Pages))
		}
		if ff.Pages[0].Format.PageLength != 60 {
			t.Errorf("pagelength = %d, want 60", ff.Pages[0].Format.PageLength)
		}
		if len(ff.Pages[0].Elements) != 0 {
			t.Errorf("elements = %d, want 0", len(ff.Pages[0].Elements))
		}
	})

	t.Run("element_outside_a_page_is_ignored", func(t *testing.T) {
		var ff FixedFormXml
		doc := `<fixedform><element><row>1</row></element><page/></fixedform>`
		if err := xml.Unmarshal([]byte(doc), &ff); err != nil {
			t.Fatalf("xml.Unmarshal: %v", err)
		}
		if len(ff.Pages) != 1 {
			t.Fatalf("pages = %d, want 1", len(ff.Pages))
		}
		if len(ff.Pages[0].Elements) != 0 {
			t.Errorf("elements = %d; elements only bind inside <page>", len(ff.Pages[0].Elements))
		}
	})
}

func TestFixedElementsSortInterface(t *testing.T) {
	t.Run("len_swap_less", func(t *testing.T) {
		els := FixedElements{
			{Row: 2, Column: 5},
			{Row: 1, Column: 9},
		}
		if els.Len() != 2 {
			t.Errorf("Len() = %d, want 2", els.Len())
		}
		// Less orders by row first, then column.
		if !els.Less(1, 0) {
			t.Error("Less(1,0) should be true: row 1 sorts before row 2")
		}
		if els.Less(0, 1) {
			t.Error("Less(0,1) should be false")
		}
		before := els[0]
		els.Swap(0, 1)
		if els[1] != before {
			t.Errorf("Swap did not exchange the elements: %+v", els)
		}
	})

	t.Run("less_uses_column_when_rows_tie", func(t *testing.T) {
		els := FixedElements{
			{Row: 4, Column: 20},
			{Row: 4, Column: 3},
		}
		if !els.Less(1, 0) {
			t.Error("Less should compare the column when the rows are equal")
		}
		if els.Less(0, 1) {
			t.Error("Less(0,1) should be false for a larger column")
		}
	})

	t.Run("identical_positions_are_not_less_either_way", func(t *testing.T) {
		els := FixedElements{
			{Row: 4, Column: 3, Content: "first"},
			{Row: 4, Column: 3, Content: "second"},
		}
		if els.Less(0, 1) || els.Less(1, 0) {
			t.Error("Less must be false in both directions for equal (row,column)")
		}
	})

	t.Run("sort_orders_rows_then_columns", func(t *testing.T) {
		els := FixedElements{
			{Row: 3, Column: 5, Content: "d"},
			{Row: 1, Column: 9, Content: "b"},
			{Row: 1, Column: 2, Content: "a"},
			{Row: 3, Column: 5, Content: "c"},
			{Row: 0, Column: 1, Content: "z"},
		}
		sort.Sort(els)
		want := []string{"z", "a", "b", "d", "c"}
		got := make([]string, len(els))
		for i, el := range els {
			got[i] = el.Content
		}
		if strings.Join(got, "") != strings.Join(want, "") {
			t.Errorf("sorted order = %v, want %v", got, want)
		}
		if !sort.IsSorted(els) {
			t.Error("the result must satisfy sort.IsSorted")
		}
	})

	t.Run("empty_and_single_slices", func(t *testing.T) {
		var empty FixedElements
		if empty.Len() != 0 {
			t.Errorf("Len() of a nil slice = %d", empty.Len())
		}
		sort.Sort(empty)
		single := FixedElements{{Row: 5, Column: 5}}
		sort.Sort(single)
		if len(single) != 1 || single[0].Row != 5 {
			t.Errorf("single element changed: %+v", single)
		}
	})
}
