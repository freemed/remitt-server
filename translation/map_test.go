package translation

// map_test.go pins the translation REGISTRY contract (map.go): the names the
// registry answers to and the name ResolveTranslator answers with.
//
// What this file pins:
//   - the exact set of registry keys: the four short names AND the four Java
//     FQCNs the legacy database stores (tTranslation.plugin,
//     tPlugins.plugin category='translation': migrations/001_legacy.up.sql:317-323
//     and :217-220; the UI harness submits the same strings,
//     ui/testHarness.html:81-89)
//   - both forms of each plugin resolve, and resolve to the SAME concrete type
//     (a fresh instance per call)
//   - an unknown name - including a plausible-looking class in the translation
//     namespace - fails with an error and a nil plugin, never a nil plugin with
//     a nil error
//   - ResolveTranslator answers with the SHORT name and does so deterministically
//     (the FQCN aliases are registry keys too, so an unordered walk would return
//     a different name - sometimes the FQCN - on different runs)
//
// The FQCNs below are the seed values themselves, not paraphrases: the Java
// classes are ../remitt/src/main/java/org/remitt/plugin/translation/{X12Xml,
// FixedFormXml,FixedFormPdf,X12Passthrough}.java, which is what makes the
// database rows valid plugin names rather than typos.

import (
	"strings"
	"testing"
)

// javaTranslators maps the string the database/UI store to the short name the
// database-free parts of the pipeline use.
var javaTranslators = []struct {
	fqcn  string
	short string
	check func(Translator) bool
	label string
}{
	{
		"org.remitt.plugin.translation.X12Xml", "x12xml",
		func(m Translator) bool { _, ok := m.(*TranslateX12Xml); return ok }, "*TranslateX12Xml",
	},
	{
		"org.remitt.plugin.translation.FixedFormXml", "fixedformxml",
		func(m Translator) bool { _, ok := m.(*TranslateFixedFormXML); return ok }, "*TranslateFixedFormXML",
	},
	{
		"org.remitt.plugin.translation.FixedFormPdf", "fixedformpdf",
		func(m Translator) bool { _, ok := m.(*TranslateFixedFormPDF); return ok }, "*TranslateFixedFormPDF",
	},
	{
		"org.remitt.plugin.translation.X12Passthrough", "x12passthrough",
		func(m Translator) bool { _, ok := m.(*TranslateX12Passthrough); return ok }, "*TranslateX12Passthrough",
	},
}

func TestRegistry_ContainsBothTheShortNamesAndTheJavaFQCNs(t *testing.T) {
	want := []string{
		"fixedformpdf",
		"fixedformxml",
		"org.remitt.plugin.translation.FixedFormPdf",
		"org.remitt.plugin.translation.FixedFormXml",
		"org.remitt.plugin.translation.X12Passthrough",
		"org.remitt.plugin.translation.X12Xml",
		"x12passthrough",
		"x12xml",
	}
	got := registeredTranslatorNames()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("registry keys = %v; want %v", got, want)
	}
}

// TestRegistry_JavaFQCNResolvesToTheSamePluginAsTheShortName is the defect this
// file exists for: tTranslation stores FQCNs, so
// InstantiateTranslator("org.remitt.plugin.translation.X12Xml") must return the
// X12 translator, not "unable to locate translator".
func TestRegistry_JavaFQCNResolvesToTheSamePluginAsTheShortName(t *testing.T) {
	for _, tt := range javaTranslators {
		t.Run(tt.fqcn, func(t *testing.T) {
			aliased, err := InstantiateTranslator(tt.fqcn)
			if err != nil {
				t.Fatalf("InstantiateTranslator(%q) = %v; the value tTranslation/tPlugins store must resolve (registered as %q)", tt.fqcn, err, tt.short)
			}
			if aliased == nil {
				t.Fatalf("InstantiateTranslator(%q) returned a nil plugin with a nil error", tt.fqcn)
			}
			if !tt.check(aliased) {
				t.Fatalf("InstantiateTranslator(%q) = %T; want %s", tt.fqcn, aliased, tt.label)
			}

			short, err := InstantiateTranslator(tt.short)
			if err != nil {
				t.Fatalf("InstantiateTranslator(%q) = %v; want the plugin", tt.short, err)
			}
			if !tt.check(short) {
				t.Fatalf("InstantiateTranslator(%q) = %T; want %s (the alias and the short name must agree)", tt.short, short, tt.label)
			}

			// Same factory, so same behaviour: each call builds a new instance.
			again, err := InstantiateTranslator(tt.fqcn)
			if err != nil {
				t.Fatalf("InstantiateTranslator(%q) on the second call = %v; want the plugin", tt.fqcn, err)
			}
			if again == aliased {
				t.Errorf("InstantiateTranslator(%q) returned the same instance twice (%p); each call must build a new plugin", tt.fqcn, aliased)
			}
		})
	}

	// The aliases are registry keys in their own right, not a lookup fallback
	// that happens to work.
	registered := map[string]bool{}
	for _, n := range registeredTranslatorNames() {
		registered[n] = true
	}
	for _, tt := range javaTranslators {
		if !registered[tt.fqcn] {
			t.Errorf("%q is not a registry key; registered names are %v", tt.fqcn, registeredTranslatorNames())
		}
		if !registered[tt.short] {
			t.Errorf("%q is not a registry key; registered names are %v", tt.short, registeredTranslatorNames())
		}
	}
}

func TestRegistry_UnknownNameReturnsErrorAndNilPlugin(t *testing.T) {
	for _, name := range []string{
		"",
		"nosuchtranslator",
		"X12Xml",  // the class name without its package is not a key
		" x12xml", // no trimming
		"x12xml ",
		JavaPluginPrefix,
		JavaPluginPrefix + "X12XmlTransport", // a plausible typo, still an error
		"org.remitt.plugin.render.XsltPlugin", // another package's FQCN
	} {
		t.Run("name="+name, func(t *testing.T) {
			m, err := InstantiateTranslator(name)
			if err == nil {
				t.Fatalf("InstantiateTranslator(%q) returned a nil error; unknown names must fail", name)
			}
			if m != nil {
				t.Fatalf("InstantiateTranslator(%q) returned a non-nil plugin (%T) alongside its error", name, m)
			}
			if !strings.Contains(err.Error(), "unable to locate translator") {
				t.Fatalf("InstantiateTranslator(%q) error = %q; want it to mention %q", name, err.Error(), "unable to locate translator")
			}
		})
	}
}

// TestResolveTranslator_AnswersWithTheShortName pins that the aliases do not
// leak into resolution answers: jobqueue logs the resolved name and passes it to
// InstantiateTranslator, and the translation map's contract is the short name.
// It runs the same resolutions repeatedly because map iteration order is
// randomised - a single pass would hide an order-dependent answer.
func TestResolveTranslator_AnswersWithTheShortName(t *testing.T) {
	tests := []struct {
		in, out string
		want    []string // accepted answers (fixedformxml -> * matches two plugins)
	}{
		{"x12xml", "x12", []string{"x12xml"}},
		{"x12xml", "*", []string{"x12xml"}},
		{"x12", "x12", []string{"x12passthrough"}},
		{"fixedformxml", "text", []string{"fixedformxml"}},
		{"fixedformxml", "pdf", []string{"fixedformpdf"}},
		{"fixedformxml", "*", []string{"fixedformxml", "fixedformpdf"}},
	}
	for _, tt := range tests {
		for i := 0; i < 25; i++ {
			got, err := ResolveTranslator(tt.in, tt.out)
			if err != nil {
				t.Fatalf("ResolveTranslator(%q, %q) = %v; want %v", tt.in, tt.out, err, tt.want)
			}
			if !containsName(tt.want, got) {
				t.Fatalf("ResolveTranslator(%q, %q) = %q; want one of %v", tt.in, tt.out, got, tt.want)
			}
			if strings.HasPrefix(got, JavaPluginPrefix) {
				t.Fatalf("ResolveTranslator(%q, %q) = %q; the FQCN alias must not be the resolved name", tt.in, tt.out, got)
			}
		}
	}
}

func TestResolveTranslator_NoMatch(t *testing.T) {
	if got, err := ResolveTranslator("nonexistent", "bogus"); err == nil {
		t.Fatalf("ResolveTranslator(nonexistent, bogus) = %q with a nil error; want an error", got)
	}
}

// TestResolveThenInstantiateRoundTrips is a guard against the obvious wrong
// fix: registering the FQCN INSTEAD of the short name would satisfy the
// database but break jobqueue's resolution path, which logs the resolved name
// and hands it straight to InstantiateTranslator.
func TestResolveThenInstantiateRoundTrips(t *testing.T) {
	name, err := ResolveTranslator("x12xml", "x12")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstantiateTranslator(name); err != nil {
		t.Fatalf("InstantiateTranslator(ResolveTranslator(\"x12xml\", \"x12\") = %q) = %v; the resolved name must instantiate", name, err)
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
