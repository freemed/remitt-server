package translation

// resolve_test.go pins the translation-resolution rule (resolve.go) with no
// database: the database seams are package variables, so each case states what
// the database would have answered and the test asserts which side of the rule
// resolved - and, crucially, that the answer is the plugin the legacy tables
// name (org.remitt.plugin.translation.*), not something the Go code invented.
//
// The live half of the evidence (the same rule evaluated against the real
// MySQL) is translation/live_resolution_test.go.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/model"
)

// resolutionStub is the database as the rule sees it: the two format lookups
// keyed by "<plugin>|<option>", the tTranslation rows keyed by inputFormat, and
// the shipped stylesheet descriptors keyed by render option.
type resolutionStub struct {
	renderFormats    map[string]string
	transportFormats map[string]string
	rows             map[string][]model.TranslationModel
	stylesheets      map[string]string

	// errors to force, so the failure paths are covered too
	renderErr    error
	transportErr error
	rowsErr      error
}

func (s *resolutionStub) install(t *testing.T) {
	t.Helper()
	prevRender, prevTransport, prevRows, prevSheet :=
		lookupRenderPluginOutputFormat, lookupTransportPluginInputFormat,
		lookupTranslationsByInputFormat, lookupStylesheetOutputFormat
	t.Cleanup(func() {
		lookupRenderPluginOutputFormat = prevRender
		lookupTransportPluginInputFormat = prevTransport
		lookupTranslationsByInputFormat = prevRows
		lookupStylesheetOutputFormat = prevSheet
	})

	lookupRenderPluginOutputFormat = func(pluginClass, pluginOption string) (string, error) {
		if s.renderErr != nil {
			return "", s.renderErr
		}
		return s.renderFormats[pluginClass+"|"+pluginOption], nil
	}
	lookupTransportPluginInputFormat = func(pluginClass, pluginOption string) (string, error) {
		if s.transportErr != nil {
			return "", s.transportErr
		}
		return s.transportFormats[pluginClass+"|"+pluginOption], nil
	}
	lookupTranslationsByInputFormat = func(inputFormat string) ([]model.TranslationModel, error) {
		if s.rowsErr != nil {
			return nil, s.rowsErr
		}
		return s.rows[inputFormat], nil
	}
	lookupStylesheetOutputFormat = func(option string) string {
		return s.stylesheets[option]
	}
}

// translationRow builds one tTranslation row the way model.GetTranslationsByInputFormat
// does.
func translationRow(plugin, in, out string) model.TranslationModel {
	return model.TranslationModel{
		Plugin:       plugin,
		InputFormat:  model.NewNullStringValue(in),
		OutputFormat: model.NewNullStringValue(out),
	}
}

// The format and plugin names below are the database's own seed values
// (migrations/001_legacy.up.sql:319-323 for tTranslation, :252-254 for
// tPluginOptions, :217-226 for tPlugins), not paraphrases.
const (
	fqcnXslt         = "org.remitt.plugin.render.XsltPlugin"
	fqcnPreRendered  = "org.remitt.plugin.render.PreRenderedPlugin"
	fqcnStoreFile    = "org.remitt.plugin.transport.StoreFile"
	fqcnStoreFilePdf = "org.remitt.plugin.transport.StoreFilePdf"
	fqcnSftp         = "org.remitt.plugin.transport.SftpTransport"

	fqcnX12Xml       = "org.remitt.plugin.translation.X12Xml"
	fqcnFixedFormXml = "org.remitt.plugin.translation.FixedFormXml"
	fqcnFixedFormPdf = "org.remitt.plugin.translation.FixedFormPdf"
)

// seedRows is tTranslation exactly as the migration seeds it.
func seedRows() map[string][]model.TranslationModel {
	return map[string][]model.TranslationModel{
		"x12xml": {translationRow(fqcnX12Xml, "x12xml", "text")},
		"fixedformxml": {
			translationRow(fqcnFixedFormPdf, "fixedformxml", "pdf"),
			translationRow(fqcnFixedFormXml, "fixedformxml", "text"),
		},
	}
}

func TestResolveTranslatorForJob_UsesTheDatabaseRow(t *testing.T) {
	cases := []struct {
		name            string
		stub            resolutionStub
		renderOption    string
		transport       string
		transportOption string
		transportGoIn   string
		wantPlugin      string
		wantType        any
	}{
		{
			// renderPluginOutputFormat(XsltPlugin, '4010_837p') = 'x12xml'
			// transportPluginInputFormat(StoreFile, NULL) = 'text'
			// tTranslation: (X12Xml, x12xml, text)
			name: "4010_837p through storefile",
			stub: resolutionStub{
				renderFormats:    map[string]string{fqcnXslt + "|4010_837p": "x12xml"},
				transportFormats: map[string]string{fqcnStoreFile + "|": "text"},
				rows:             seedRows(),
			},
			renderOption: "4010_837p", transport: fqcnStoreFile, transportGoIn: "*",
			wantPlugin: fqcnX12Xml, wantType: &TranslateX12Xml{},
		},
		{
			// The same pair as the Java's own unit test:
			// ConfigurationTest.testResolveTranslationPlugin (ConfigurationTest.java:51-68).
			name: "5010_837p through the scripted http transport",
			stub: resolutionStub{
				renderFormats:    map[string]string{fqcnXslt + "|5010_837p": "x12xml"},
				transportFormats: map[string]string{"org.remitt.plugin.transport.ScriptedHttpTransport|ClaimLogic": "text"},
				rows:             seedRows(),
			},
			renderOption: "5010_837p", transport: "org.remitt.plugin.transport.ScriptedHttpTransport", transportOption: "ClaimLogic", transportGoIn: "x12",
			wantPlugin: fqcnX12Xml, wantType: &TranslateX12Xml{},
		},
		{
			// cms1500 declares 'fixedformxml'; the text row is FixedFormXml and
			// the pdf row FixedFormPdf, so the transport's declared input format
			// decides between two rows with the same inputFormat.
			name: "cms1500 through storefile (text)",
			stub: resolutionStub{
				renderFormats:    map[string]string{fqcnXslt + "|cms1500": "fixedformxml"},
				transportFormats: map[string]string{fqcnStoreFile + "|": "text"},
				rows:             seedRows(),
			},
			renderOption: "cms1500", transport: fqcnStoreFile, transportGoIn: "*",
			wantPlugin: fqcnFixedFormXml, wantType: &TranslateFixedFormXML{},
		},
		{
			name: "cms1500 through storefilepdf (pdf)",
			stub: resolutionStub{
				renderFormats:    map[string]string{fqcnXslt + "|cms1500": "fixedformxml"},
				transportFormats: map[string]string{fqcnStoreFilePdf + "|": "pdf"},
				rows:             seedRows(),
			},
			renderOption: "cms1500", transport: fqcnStoreFilePdf, transportGoIn: "pdf",
			wantPlugin: fqcnFixedFormPdf, wantType: &TranslateFixedFormPDF{},
		},
		{
			// A transport whose declared input format is a LIST: FIND_IN_SET's
			// membership test, not string equality.
			name: "a multi-format transport input list",
			stub: resolutionStub{
				renderFormats:    map[string]string{fqcnXslt + "|cms1500": "fixedformxml"},
				transportFormats: map[string]string{fqcnStoreFilePdf + "|": "pdf,text"},
				rows:             seedRows(),
			},
			renderOption: "cms1500", transport: fqcnStoreFilePdf, transportGoIn: "pdf",
			wantPlugin: fqcnFixedFormPdf, wantType: &TranslateFixedFormPDF{},
		},
		{
			// The same row with different case: the columns are
			// utf8mb4_0900_ai_ci, so MySQL's FIND_IN_SET would match.
			name: "case-insensitive membership",
			stub: resolutionStub{
				renderFormats:    map[string]string{fqcnXslt + "|4010_837p": "x12xml"},
				transportFormats: map[string]string{fqcnStoreFile + "|": "TEXT"},
				rows:             seedRows(),
			},
			renderOption: "4010_837p", transport: fqcnStoreFile, transportGoIn: "x12",
			wantPlugin: fqcnX12Xml, wantType: &TranslateX12Xml{},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.stub.install(t)
			name, source, err := ResolveTranslatorForJob(fqcnXslt, tt.renderOption, tt.transport, tt.transportOption, tt.transportGoIn)
			if err != nil {
				t.Fatalf("ResolveTranslatorForJob(%q, %q, %q, %q, %q) = %v; want %q", fqcnXslt, tt.renderOption, tt.transport, tt.transportOption, tt.transportGoIn, err, tt.wantPlugin)
			}
			if name != tt.wantPlugin {
				t.Errorf("resolved %q; want %q", name, tt.wantPlugin)
			}
			if source != SourceDatabase {
				t.Errorf("source = %q; want %q (the database seeded this pair)", source, SourceDatabase)
			}
			// The name the database stores must instantiate, and to the plugin
			// the row means.
			m, err := InstantiateTranslator(name)
			if err != nil {
				t.Fatalf("InstantiateTranslator(%q) [the resolved name]: %v", name, err)
			}
			if got := typeName(m); got != typeName(tt.wantType) {
				t.Errorf("InstantiateTranslator(%q) = %s; want %s", name, got, typeName(tt.wantType))
			}
		})
	}
}

func typeName(v any) string {
	return fmt.Sprintf("%T", v)
}

func TestResolveTranslatorForJob_FallsBackToTheRegistriesWhenTheDatabaseHasNoRow(t *testing.T) {
	// statement: renderPluginOutputFormat(XsltPlugin,'statement') = 'statementxml'
	// (tPluginOptions), and tTranslation has NO row for 'statementxml' - the
	// table is (fixedformxml,pdf), (fixedformxml,text), (x12xml,text). The
	// shipped statement.xsl.xml declares the stylesheet's own output as
	// fixedformxml, so the Go side's declaration resolves it.
	stub := &resolutionStub{
		renderFormats:    map[string]string{fqcnXslt + "|statement": "statementxml"},
		transportFormats: map[string]string{fqcnStoreFile + "|": "text"},
		rows:             seedRows(),
		stylesheets:      map[string]string{"statement": "fixedformxml"},
	}
	stub.install(t)

	name, source, err := ResolveTranslatorForJob(fqcnXslt, "statement", fqcnStoreFile, "", "*")
	if err != nil {
		t.Fatalf("ResolveTranslatorForJob(statement / storefile) = %v; want the stylesheet-declared fallback to resolve it", err)
	}
	if source != SourceGoRegistry {
		t.Errorf("source = %q; want %q (no tTranslation row names statementxml)", source, SourceGoRegistry)
	}
	if name != "fixedformxml" {
		t.Errorf("resolved %q; want the short name 'fixedformxml' (ResolveTranslator answers with short names)", name)
	}
	if m, err := InstantiateTranslator(name); err != nil {
		t.Errorf("InstantiateTranslator(%q) = %v", name, err)
	} else if _, ok := m.(*TranslateFixedFormXML); !ok {
		t.Errorf("InstantiateTranslator(%q) = %T; want *TranslateFixedFormXML", name, m)
	}

	// The statement stylesheet printed to PDF: the same fallback reaches the
	// pdf translator, because the transport's declared input format is 'pdf'.
	stubPDF := &resolutionStub{
		renderFormats:    map[string]string{fqcnXslt + "|statement": "statementxml"},
		transportFormats: map[string]string{fqcnStoreFilePdf + "|": "pdf"},
		rows:             seedRows(),
		stylesheets:      map[string]string{"statement": "fixedformxml"},
	}
	stubPDF.install(t)
	name, source, err = ResolveTranslatorForJob(fqcnXslt, "statement", fqcnStoreFilePdf, "", "pdf")
	if err != nil {
		t.Fatalf("ResolveTranslatorForJob(statement / storefilepdf) = %v", err)
	}
	if name != "fixedformpdf" || source != SourceGoRegistry {
		t.Errorf("statement -> %q (%s); want 'fixedformpdf' (%s)", name, source, SourceGoRegistry)
	}
}

func TestResolveTranslatorForJob_FallbackKeepsWorkingWhenTheDatabaseSeededNoTranslatorRow(t *testing.T) {
	// PreRenderedPlugin declares outputFormat 'x12' and the database has no
	// tTranslation row for 'x12' at all, while the x12 passthrough translator
	// is registered against 'x12' -> 'x12'/'*'. This is the pair the pipeline's
	// live test drives, so it must keep resolving.
	cases := []struct {
		name           string
		transport      string
		transportGoIn  string
		wantShortNames []string
	}{
		{"sftp (database 'text', Go 'x12')", fqcnSftp, "x12", []string{"x12passthrough"}},
		{"storefile (database 'text', Go '*')", fqcnStoreFile, "*", []string{"x12passthrough"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			stub := &resolutionStub{
				renderFormats:    map[string]string{fqcnPreRendered + "|x12": "x12"},
				transportFormats: map[string]string{tt.transport + "|": "text"},
				rows:             seedRows(),
			}
			stub.install(t)
			name, source, err := ResolveTranslatorForJob(fqcnPreRendered, "x12", tt.transport, "", tt.transportGoIn)
			if err != nil {
				t.Fatalf("ResolveTranslatorForJob(PreRendered/x12 / %s) = %v", tt.transport, err)
			}
			if source != SourceGoRegistry {
				t.Errorf("source = %q; want %q", source, SourceGoRegistry)
			}
			found := false
			for _, want := range tt.wantShortNames {
				if name == want {
					found = true
				}
			}
			if !found {
				t.Errorf("resolved %q; want one of %v", name, tt.wantShortNames)
			}
			if _, err := InstantiateTranslator(name); err != nil {
				t.Errorf("InstantiateTranslator(%q) = %v", name, err)
			}
		})
	}
}

func TestResolveTranslatorForJob_NoTranslatorIsAnErrorThatSaysWhy(t *testing.T) {
	// 4010_837p renders x12xml; StoreFilePdf accepts only 'pdf'. The database
	// has a row for x12xml (X12Xml -> text) and it is not acceptable to a pdf
	// transport, and no registered translator turns x12xml into pdf - so
	// nothing resolves, which is exactly the case the Java marked failed
	// (RenderProcessorThread.java:100-118).
	stub := &resolutionStub{
		renderFormats:    map[string]string{fqcnXslt + "|4010_837p": "x12xml"},
		transportFormats: map[string]string{fqcnStoreFilePdf + "|": "pdf"},
		rows:             seedRows(),
	}
	stub.install(t)

	name, source, err := ResolveTranslatorForJob(fqcnXslt, "4010_837p", fqcnStoreFilePdf, "", "pdf")
	if err == nil {
		t.Fatalf("ResolveTranslatorForJob(4010_837p / storefilepdf) = %q (%s) with a nil error; x12 cannot become a pdf and nothing may resolve", name, source)
	}
	if name != "" || source != "" {
		t.Errorf("a failed resolution returned name=%q source=%q; want both empty", name, source)
	}
	for _, want := range []string{
		"unable to resolve translator between 'x12xml' and 'pdf'",
		"tTranslation has 1 row(s) for inputFormat \"x12xml\"",
		"the Go fallback found no translator",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text %q does not contain %q; the failure must say why", err.Error(), want)
		}
	}

	// The other shape of "nothing can resolve": the database declares no format
	// for the render option at all, so there is not even a pair to look up.
	stubNoFormat := &resolutionStub{rows: seedRows()}
	stubNoFormat.install(t)
	name, source, err = ResolveTranslatorForJob(fqcnXslt, "bogus", fqcnStoreFilePdf, "", "pdf")
	if err == nil {
		t.Fatalf("ResolveTranslatorForJob(option that does not exist) = %q (%s) with a nil error", name, source)
	}
	for _, want := range []string{
		"unable to resolve translator between 'bogus' and 'pdf'",
		"the database declares no output format for render plugin",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text %q does not contain %q", err.Error(), want)
		}
	}

	// The same, for a plugin whose tPlugins format is the 'various' sentinel
	// with no tPluginOptions row for the option - what the stored function
	// answers for an option that does not exist (MySQL's SELECT INTO leaves its
	// target alone, so it returns 'various' rather than NULL). 'various' is not
	// a format and must not be looked up in tTranslation as one.
	stubSentinel := &resolutionStub{
		renderFormats:    map[string]string{fqcnXslt + "|nosuchoption": "various"},
		transportFormats: map[string]string{fqcnStoreFile + "|": "text"},
		rows:             seedRows(),
	}
	stubSentinel.install(t)
	name, source, err = ResolveTranslatorForJob(fqcnXslt, "nosuchoption", fqcnStoreFile, "", "*")
	if err == nil {
		t.Fatalf("ResolveTranslatorForJob(various sentinel) = %q (%s) with a nil error", name, source)
	}
	for _, want := range []string{
		"the 'various' sentinel and tPluginOptions has no row for option \"nosuchoption\"",
		"the Go fallback found no translator",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text %q does not contain %q", err.Error(), want)
		}
	}
}

func TestResolveTranslatorForJob_PropagatesLookupFailures(t *testing.T) {
	// A database that answers with an error must fail the resolution with that
	// error (the caller marks the payload failed), never fall through to the Go
	// registries - a translator invented for an unreachable database would hide
	// a broken deployment.
	stub := &resolutionStub{rowsErr: errStub}
	stub.install(t)
	if _, _, err := ResolveTranslatorForJob(fqcnXslt, "4010_837p", fqcnStoreFile, "", "*"); err == nil {
		t.Fatal("ResolveTranslatorForJob with a failing tTranslation lookup returned a nil error")
	}
}

var errStub = &stubError{"stub database failure"}

type stubError struct{ s string }

func (e *stubError) Error() string { return e.s }

// TestFormatInSet pins MySQL's FIND_IN_SET semantics for the values this schema
// stores.
func TestFormatInSet(t *testing.T) {
	cases := []struct {
		format, list string
		want         bool
		why          string
	}{
		{"text", "text", true, "the seeded pair"},
		{"text", "pdf,text", true, "FIND_IN_SET looks at every element"},
		{"pdf", "pdf,text", true, ""},
		{"text", "TEXT", true, "utf8mb4_0900_ai_ci is case-insensitive"},
		{"TEXT", "text", true, ""},
		{"text", "pdf", false, "the seeded 4010_837p -> storefilepdf case"},
		{"text", " text", false, "FIND_IN_SET does not trim; a leading space is part of the element"},
		{"text", "", false, "FIND_IN_SET(str,'') is 0"},
		{"", "text", false, "FIND_IN_SET('','text') is 0"},
		{"", "", false, ""},
		{"tex", "text", false, "not a prefix match"},
		{"x12xml", "x12xml", true, ""},
	}
	for _, tt := range cases {
		if got := FormatInSet(tt.format, tt.list); got != tt.want {
			t.Errorf("FormatInSet(%q, %q) = %v; want %v %s", tt.format, tt.list, got, tt.want, tt.why)
		}
	}
}

// TestStylesheetOutputFormat reads the SHIPPED descriptors out of the tree, so
// the fallback's other half is pinned to real files: the four stylesheet
// options and their declared output formats (the Java files these were copied
// from: ../remitt/src/main/webapp/WEB-INF/xsl/*.xsl.xml).
func TestStylesheetOutputFormat(t *testing.T) {
	prev := config.Config
	t.Cleanup(func() { config.Config = prev })
	cfg := &config.AppConfig{}
	cfg.Paths.BasePath = ".."
	config.Config = cfg

	cases := map[string]string{
		"4010_837p":   "x12xml",
		"5010_837p":   "x12xml",
		"cms1500":     "fixedformxml",
		"statement":   "fixedformxml",
		"":            "",
		"nosuchstyle": "",
	}
	for option, want := range cases {
		if got := StylesheetOutputFormat(option); got != want {
			t.Errorf("StylesheetOutputFormat(%q) = %q; want %q", option, got, want)
		}
	}

	// Without a configured base path there is nothing to read, and the fallback
	// must answer "no declaration" rather than invent one.
	config.Config = nil
	if got := StylesheetOutputFormat("statement"); got != "" {
		t.Errorf("StylesheetOutputFormat(statement) with no config = %q; want \"\"", got)
	}
}

// TestResolveTranslatorForJob_BehaviourWhenTheDatabaseIsAbsent pins that an
// uninitialised model (no MySQL in this process) is reported by the model
// layer, not by a panic: every unit test above stubs the seams, so this is the
// one case that reaches model's own guard.
func TestResolveTranslatorForJob_BehaviourWhenTheDatabaseIsAbsent(t *testing.T) {
	prevRender, prevTransport, prevRows, prevSheet :=
		lookupRenderPluginOutputFormat, lookupTransportPluginInputFormat,
		lookupTranslationsByInputFormat, lookupStylesheetOutputFormat
	t.Cleanup(func() {
		lookupRenderPluginOutputFormat = prevRender
		lookupTransportPluginInputFormat = prevTransport
		lookupTranslationsByInputFormat = prevRows
		lookupStylesheetOutputFormat = prevSheet
	})
	lookupRenderPluginOutputFormat = model.RenderPluginOutputFormat
	lookupTransportPluginInputFormat = model.TransportPluginInputFormat
	lookupTranslationsByInputFormat = model.GetTranslationsByInputFormat

	if model.Queries != nil {
		t.Skip("a live database handle is installed in this process; the no-database guard is the model unit test's subject")
	}
	if _, _, err := ResolveTranslatorForJob(fqcnXslt, "4010_837p", fqcnStoreFile, "", "*"); err == nil {
		t.Fatal("ResolveTranslatorForJob with no database returned a nil error")
	} else if !strings.Contains(err.Error(), "database is not initialised") {
		t.Errorf("error %q does not name the uninitialised database", err.Error())
	}
}
