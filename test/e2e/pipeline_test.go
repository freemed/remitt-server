package e2e

import (
	"context"
	"encoding/xml"
	"os"
	"testing"

	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
	"github.com/freemed/remitt-server/render"
	"github.com/freemed/remitt-server/translation"
	"github.com/freemed/remitt-server/transport"
)

// testUser creates a minimal UserModel and context for tests that
// need a user context (transport, etc.).
func testUser() (*model.UserModel, context.Context) {
	u := &model.UserModel{
		Id:       1,
		Username: "testuser",
	}
	ctx := user.NewContext(context.Background(), u)
	return u, ctx
}

// ---------------------------------------------------------------------------
// Render Plugin Tests
// ---------------------------------------------------------------------------

func TestRender_PreRenderedPlugin(t *testing.T) {
	r, err := render.InstantiateRenderer("org.remitt.plugin.render.PreRenderedPlugin")
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	r.SetContext(context.Background())

	input := []byte("<test>passthrough data</test>")
	out, err := r.Render(input, "any")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if string(out) != string(input) {
		t.Errorf("PreRenderedPlugin should pass through unchanged; got %q, want %q", string(out), string(input))
	}
}

// ---------------------------------------------------------------------------
// Translation Plugin Tests
// ---------------------------------------------------------------------------

func TestTranslation_X12Xml(t *testing.T) {
	// Read test data
	data, err := os.ReadFile("../../test/testdata/x12_intermediate.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var x12xml model.X12Xml
	if err := xml.Unmarshal(data, &x12xml); err != nil {
		t.Fatalf("unmarshal X12Xml: %v", err)
	}

	translator, err := translation.InstantiateTranslator("x12xml")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	out, err := translator.Translate(x12xml)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	// Verify we got X12 EDI output
	outStr := string(out)
	if len(outStr) == 0 {
		t.Error("expected non-empty X12 output")
	}
	// Verify key segments are present
	for _, seg := range []string{"ISA*", "GS*", "ST*835*", "BPR*", "TRN*", "CLP*", "SE*", "GE*", "IEA*"} {
		if !contains(outStr, seg) {
			t.Errorf("expected segment %q in output", seg)
		}
	}
	// Verify delimiter is present
	if !contains(outStr, "*") {
		t.Error("expected * delimiter in X12 output")
	}
}

func TestTranslation_X12Passthrough(t *testing.T) {
	data, err := os.ReadFile("../../test/testdata/sample_835.x12")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	translator, err := translation.InstantiateTranslator("x12passthrough")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	out, err := translator.Translate(data)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if string(out) != string(data) {
		t.Errorf("X12Passthrough should pass through unchanged")
	}
}

func TestTranslation_X12Passthrough_Resolver(t *testing.T) {
	translator, err := translation.InstantiateTranslator("x12passthrough")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}

	tests := []struct {
		in, out string
		want    bool
	}{
		{"x12", "x12", true},
		{"x12", "*", true},
		{"x12xml", "x12", false},
		{"fixedformxml", "text", false},
	}
	for _, tt := range tests {
		if got := translator.Resolver(tt.in, tt.out); got != tt.want {
			t.Errorf("Resolver(%q, %q) = %v; want %v", tt.in, tt.out, got, tt.want)
		}
	}
}

func TestTranslation_FixedFormXML(t *testing.T) {
	data, err := os.ReadFile("../../test/testdata/fixedform_simple.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var ff model.FixedFormXml
	if err := xml.Unmarshal(data, &ff); err != nil {
		t.Fatalf("unmarshal FixedFormXml: %v", err)
	}

	translator, err := translation.InstantiateTranslator("fixedformxml")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	out, err := translator.Translate(ff)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	// Verify output is non-empty and contains key text
	outStr := string(out)
	if len(outStr) == 0 {
		t.Error("expected non-empty fixed-form text output")
	}
	if !contains(outStr, "REMITT TEST FORM") {
		t.Error("expected 'REMITT TEST FORM' in output")
	}
	if !contains(outStr, "JOHN Q DOE") {
		t.Error("expected 'JOHN Q DOE' in output")
	}
}

func TestTranslation_FixedFormPDF(t *testing.T) {
	data, err := os.ReadFile("../../test/testdata/fixedform_simple.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var ff model.FixedFormXml
	if err := xml.Unmarshal(data, &ff); err != nil {
		t.Fatalf("unmarshal FixedFormXml: %v", err)
	}

	translator, err := translation.InstantiateTranslator("fixedformpdf")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	// Set template path
	if tpdf, ok := translator.(*translation.TranslateFixedFormPDF); ok {
		tpdf.TemplatePath = "../../resources/pdf"
	}

	out, err := translator.Translate(ff)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	if len(out) == 0 {
		t.Error("expected non-empty PDF output")
	}
	// PDF should start with %PDF magic bytes
	if len(out) < 5 || string(out[:5]) != "%PDF-" {
		t.Errorf("expected PDF magic bytes, got %q", string(out[:min(len(out), 20)]))
	}
}

// ---------------------------------------------------------------------------
// ResolveTranslator Tests
// ---------------------------------------------------------------------------

func TestResolveTranslator(t *testing.T) {
	tests := []struct {
		in, out string
		want    string
	}{
		{"x12xml", "x12", "x12xml"},
		{"x12xml", "*", "x12xml"},
		{"x12", "x12", "x12passthrough"},
		{"x12", "*", "x12passthrough"},
		{"fixedformxml", "text", "fixedformxml"},
		{"fixedformxml", "pdf", "fixedformpdf"},
		// fixedformxml → * could resolve to either, skip exact match
	}
	for _, tt := range tests {
		got, err := translation.ResolveTranslator(tt.in, tt.out)
		if err != nil {
			t.Errorf("ResolveTranslator(%q, %q): unexpected error: %v", tt.in, tt.out, err)
			continue
		}
		// Special case: fixedformxml → * maps to either fixedformxml or fixedformpdf
		if tt.in == "fixedformxml" && tt.out == "*" {
			if got != "fixedformxml" && got != "fixedformpdf" {
				t.Errorf("ResolveTranslator(%q, %q) = %q; want 'fixedformxml' or 'fixedformpdf'", tt.in, tt.out, got)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("ResolveTranslator(%q, %q) = %q; want %q", tt.in, tt.out, got, tt.want)
		}
	}
}

func TestResolveTranslator_NoMatch(t *testing.T) {
	_, err := translation.ResolveTranslator("nonexistent", "bogus")
	if err == nil {
		t.Error("expected error for unresolvable translator pair")
	}
}

// ---------------------------------------------------------------------------
// Transport Plugin Tests (non-external only)
// ---------------------------------------------------------------------------

func TestTransport_Script_Instantiation(t *testing.T) {
	tr, err := transport.InstantiateTransporter("script")
	if err != nil {
		t.Fatalf("instantiate transporter: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transporter")
	}

	// Verify InputFormat
	if fmt := tr.InputFormat(); fmt != "x12" {
		t.Errorf("Script InputFormat() = %q; want %q", fmt, "x12")
	}

	// Verify Options
	opts := tr.Options()
	if len(opts) == 0 {
		t.Error("expected non-empty options for Script transporter")
	}
}

func TestTransport_StoreFile_Instantiation(t *testing.T) {
	tr, err := transport.InstantiateTransporter("storefile")
	if err != nil {
		t.Fatalf("instantiate transporter: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transporter")
	}

	if fmt := tr.InputFormat(); fmt != "*" {
		t.Errorf("StoreFile InputFormat() = %q; want %q", fmt, "*")
	}
}

func TestTransport_StoreFilePdf_Instantiation(t *testing.T) {
	tr, err := transport.InstantiateTransporter("storefilepdf")
	if err != nil {
		t.Fatalf("instantiate transporter: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transporter")
	}

	if fmt := tr.InputFormat(); fmt != "pdf" {
		t.Errorf("StoreFilePdf InputFormat() = %q; want %q", fmt, "pdf")
	}
}

func TestTransport_Script(t *testing.T) {
	u, ctx := testUser()

	tr, err := transport.InstantiateTransporter("script")
	if err != nil {
		t.Fatalf("instantiate transporter: %v", err)
	}
	tr.SetContext(ctx)

	err = tr.SetOptions(map[string]any{
		"script": `log("test script transport: data length = " + String(data).length);`,
		"timeout": 5,
	})
	if err != nil {
		t.Fatalf("set options: %v", err)
	}

	// Transport some test data through the script
	err = tr.Transport("testfile.x12", []byte("TEST DATA PAYLOAD"))
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	_ = u
}

func TestTransport_Script_MissingOptions(t *testing.T) {
	_, ctx := testUser()

	tr, err := transport.InstantiateTransporter("script")
	if err != nil {
		t.Fatalf("instantiate transporter: %v", err)
	}
	tr.SetContext(ctx)

	// Don't set options — should fail on transport
	err = tr.Transport("testfile.x12", []byte("data"))
	if err == nil {
		t.Error("expected error when script option is missing")
	}
}

// ---------------------------------------------------------------------------
// Full Pipeline Integration Tests
// ---------------------------------------------------------------------------

// TestPipeline_PreRendered_X12Passthrough_ToBytes tests:
//   PreRenderedPlugin → X12Passthrough → byte output (no transport)
func TestPipeline_PreRendered_X12Passthrough_ToBytes(t *testing.T) {
	rawX12, err := os.ReadFile("../../test/testdata/sample_835.x12")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	// Stage 1: Render (PreRenderedPlugin passthrough)
	renderer, err := render.InstantiateRenderer("org.remitt.plugin.render.PreRenderedPlugin")
	if err != nil {
		t.Fatalf("instantiate renderer: %v", err)
	}
	renderer.SetContext(context.Background())

	rendered, err := renderer.Render(rawX12, "x12")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if string(rendered) != string(rawX12) {
		t.Error("pre-rendered passthrough should preserve data")
	}

	// Stage 2: Translate (X12Passthrough)
	translator, err := translation.InstantiateTranslator("x12passthrough")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	translated, err := translator.Translate(rendered)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if string(translated) != string(rawX12) {
		t.Error("x12passthrough should preserve data")
	}
}

// TestPipeline_X12Xml_X12Xml_Translation tests:
//   X12Xml structured data → X12Xml translator → X12 EDI text output
func TestPipeline_X12Xml_ToEDI(t *testing.T) {
	data, err := os.ReadFile("../../test/testdata/x12_intermediate.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var x12doc model.X12Xml
	if err := xml.Unmarshal(data, &x12doc); err != nil {
		t.Fatalf("unmarshal X12Xml: %v", err)
	}

	translator, err := translation.InstantiateTranslator("x12xml")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	out, err := translator.Translate(x12doc)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	// Verify the output is valid X12 EDI
	outStr := string(out)
	if len(outStr) == 0 {
		t.Fatal("expected non-empty X12 output")
	}

	// Verify envelope structure
	if !contains(outStr, "ISA*") {
		t.Error("missing ISA segment")
	}
	if !contains(outStr, "IEA*") {
		t.Error("missing IEA segment")
	}
	if !contains(outStr, "~") {
		t.Error("missing segment terminator (~)")
	}
}

// TestPipeline_FixedFormXML_ToText tests:
//   FixedFormXml structured data → FixedFormXML translator → fixed-width text
func TestPipeline_FixedFormXML_ToText(t *testing.T) {
	data, err := os.ReadFile("../../test/testdata/fixedform_simple.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var ff model.FixedFormXml
	if err := xml.Unmarshal(data, &ff); err != nil {
		t.Fatalf("unmarshal FixedFormXml: %v", err)
	}

	if len(ff.Pages) == 0 {
		t.Fatal("expected at least one page in test data")
	}

	translator, err := translation.InstantiateTranslator("fixedformxml")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	out, err := translator.Translate(ff)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	outStr := string(out)
	if len(outStr) == 0 {
		t.Fatal("expected non-empty fixed-form text output")
	}

	// Verify content from both pages (content may be truncated to element length)
	for _, s := range []string{"REMITT TEST FORM", "JOHN Q DOE", "CLM-2024-001234", "TEST MEDICAL GROUP", "J45.909"} {
		if !contains(outStr, s) {
			t.Errorf("expected %q in output", s)
		}
	}
}

// TestPipeline_FixedFormXML_ToPDF tests:
//   FixedFormXml structured data → FixedFormPDF translator → PDF output
func TestPipeline_FixedFormXML_ToPDF(t *testing.T) {
	data, err := os.ReadFile("../../test/testdata/fixedform_simple.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var ff model.FixedFormXml
	if err := xml.Unmarshal(data, &ff); err != nil {
		t.Fatalf("unmarshal FixedFormXml: %v", err)
	}

	translator, err := translation.InstantiateTranslator("fixedformpdf")
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	// Set template path for PDF generation
	if tpdf, ok := translator.(*translation.TranslateFixedFormPDF); ok {
		tpdf.TemplatePath = "../../resources/pdf"
	}

	out, err := translator.Translate(ff)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	// Verify PDF magic bytes
	if len(out) < 5 || string(out[:5]) != "%PDF-" {
		t.Errorf("expected PDF output, got %q", string(out[:min(len(out), 20)]))
	}
	if len(out) < 100 {
		t.Errorf("expected substantial PDF output, got %d bytes", len(out))
	}
}

// TestPipeline_X12Xml_ResolvedTranslation verifies the resolver chain:
//   x12xml → resolve to "x12xml" translator → verify it handles the data
func TestPipeline_X12Xml_ResolvedTranslation(t *testing.T) {
	// Resolve the translator
	name, err := translation.ResolveTranslator("x12xml", "x12")
	if err != nil {
		t.Fatalf("resolve translator: %v", err)
	}
	if name != "x12xml" {
		t.Errorf("expected 'x12xml', got %q", name)
	}

	translator, err := translation.InstantiateTranslator(name)
	if err != nil {
		t.Fatalf("instantiate translator: %v", err)
	}
	translator.SetContext(context.Background())

	// Load test data
	data, err := os.ReadFile("../../test/testdata/x12_intermediate.xml")
	if err != nil {
		t.Fatalf("read test data: %v", err)
	}

	var x12doc model.X12Xml
	if err := xml.Unmarshal(data, &x12doc); err != nil {
		t.Fatalf("unmarshal X12Xml: %v", err)
	}

	out, err := translator.Translate(x12doc)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(out) == 0 {
		t.Error("expected non-empty translation output")
	}
}

// ---------------------------------------------------------------------------
// Plugin Registry Tests
// ---------------------------------------------------------------------------

func TestInstantiateRenderer_All(t *testing.T) {
	names := []string{
		"org.remitt.plugin.render.PreRenderedPlugin",
		"org.remitt.plugin.render.XsltPlugin",
	}
	for _, name := range names {
		r, err := render.InstantiateRenderer(name)
		if err != nil {
			t.Errorf("InstantiateRenderer(%q): %v", name, err)
			continue
		}
		if r == nil {
			t.Errorf("InstantiateRenderer(%q): nil return", name)
		}
	}
}

func TestInstantiateTranslator_All(t *testing.T) {
	names := []string{
		"x12xml",
		"x12passthrough",
		"fixedformxml",
		"fixedformpdf",
	}
	for _, name := range names {
		tr, err := translation.InstantiateTranslator(name)
		if err != nil {
			t.Errorf("InstantiateTranslator(%q): %v", name, err)
			continue
		}
		if tr == nil {
			t.Errorf("InstantiateTranslator(%q): nil return", name)
		}
	}
}

func TestInstantiateTransporter_NonExternal(t *testing.T) {
	names := []string{
		"storefile",
		"storefilepdf",
		"script",
	}
	for _, name := range names {
		tr, err := transport.InstantiateTransporter(name)
		if err != nil {
			t.Errorf("InstantiateTransporter(%q): %v", name, err)
			continue
		}
		if tr == nil {
			t.Errorf("InstantiateTransporter(%q): nil return", name)
		}
	}
}

func TestInstantiateTransporter_External_Exists(t *testing.T) {
	// External transporters exist in the registry but we skip their
	// Transport() calls since they require real SFTP servers.
	names := []string{
		"sftp",
		"claimlogic",
		"gatewayedi",
	}
	for _, name := range names {
		tr, err := transport.InstantiateTransporter(name)
		if err != nil {
			t.Errorf("InstantiateTransporter(%q) should exist in registry: %v", name, err)
			continue
		}
		if tr == nil {
			t.Errorf("InstantiateTransporter(%q): nil return", name)
		}
	}
}

// ---------------------------------------------------------------------------
// InputFormat Contract Tests
// ---------------------------------------------------------------------------

func TestTransport_InputFormats(t *testing.T) {
	tests := []struct {
		name   string
		format string
	}{
		{"storefile", "*"},
		{"storefilepdf", "pdf"},
		{"script", "x12"},
		{"sftp", "x12"},
		{"claimlogic", "x12"},
		{"gatewayedi", "x12"},
	}
	for _, tt := range tests {
		tr, err := transport.InstantiateTransporter(tt.name)
		if err != nil {
			t.Errorf("instantiate %q: %v", tt.name, err)
			continue
		}
		if got := tr.InputFormat(); got != tt.format {
			t.Errorf("%s.InputFormat() = %q; want %q", tt.name, got, tt.format)
		}
	}
}

// ---------------------------------------------------------------------------
// Translation Resolver Contract Tests
// ---------------------------------------------------------------------------

func TestTranslation_Resolvers(t *testing.T) {
	tests := []struct {
		translator string
		in, out    string
		want       bool
	}{
		{"x12xml", "x12xml", "x12", true},
		{"x12xml", "x12xml", "*", true},
		{"x12xml", "x12xml", "pdf", false},
		{"x12passthrough", "x12", "x12", true},
		{"x12passthrough", "x12", "*", true},
		{"x12passthrough", "x12xml", "x12", false},
		{"fixedformxml", "fixedformxml", "text", true},
		{"fixedformxml", "fixedformxml", "*", true},
		{"fixedformxml", "fixedformxml", "pdf", false},
		{"fixedformpdf", "fixedformxml", "pdf", true},
		{"fixedformpdf", "fixedformxml", "*", true},
		{"fixedformpdf", "fixedformxml", "text", false},
	}
	for _, tt := range tests {
		tr, err := translation.InstantiateTranslator(tt.translator)
		if err != nil {
			t.Errorf("instantiate %q: %v", tt.translator, err)
			continue
		}
		if got := tr.Resolver(tt.in, tt.out); got != tt.want {
			t.Errorf("%s.Resolver(%q, %q) = %v; want %v", tt.translator, tt.in, tt.out, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
