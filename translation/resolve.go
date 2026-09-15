package translation

// resolve.go implements the translation-resolution rule the legacy database
// owned, so the render options the shipped stylesheets declare can actually
// reach a transport.
//
// WHAT THE JAVA DID
//
// The Java never compared format strings in the process. ControlThread resolved
// the translation stage per payload -
//
//	Configuration.resolveTranslationPlugin(payload.getRenderPlugin(),
//	    payload.getRenderOption(), payload.getTransportPlugin(),
//	    payload.getTransportOption())
//	(ControlThread.java:667-686) -> CALL p_ResolveTranslationPlugin(?,?,?,?)
//	(Configuration.java:229-262)
//
// - and the procedure (migrations/001_legacy.up.sql:327-338) did the work in
// SQL:
//
//	SELECT plugin FROM tTranslation WHERE
//	    inputFormat = renderPluginOutputFormat( renderPlugin, renderOption )
//	    AND FIND_IN_SET( outputFormat, transportPluginInputFormat( transportPlugin, transportOption ) )
//	    LIMIT 1;
//
// with renderPluginOutputFormat (:276-291) reading tPlugins.outputFormat for
// the plugin class and, when that value is the 'various' sentinel, the option's
// row in tPluginOptions; transportPluginInputFormat (:293-310) is the same
// shape over tPlugins.inputFormat. Nothing in that path ever looks at a format
// string the Go registries declare.
//
// WHY THE GO PORT COULD NOT RESOLVE ANYTHING
//
// The Go pipeline called ResolveTranslator(w.RenderOption,
// transportPlugin.InputFormat()) - the render OPTION where the database uses
// the option's declared OUTPUT FORMAT, and the Go transport's own format string
// where the database uses tPlugins.inputFormat. The two vocabularies do not
// meet: the database says
//
//	renderPluginOutputFormat('org.remitt.plugin.render.XsltPlugin','4010_837p') = 'x12xml'
//	transportPluginInputFormat('org.remitt.plugin.transport.StoreFile', NULL)   = 'text'
//
// while the Go registries say '4010_837p' -> ? and SftpTransport.InputFormat()
// = 'x12'. Every shipped stylesheet therefore failed with "unable to resolve
// translator between '4010_837p' and 'x12'".
//
// WHAT THIS FILE DOES
//
// ResolveTranslatorForJob answers with the database's own mapping first (the
// rule above, evaluated against tPlugins/tPluginOptions/tTranslation through
// model.RenderPluginOutputFormat, model.TransportPluginInputFormat and
// model.GetTranslationsByInputFormat), and only when the database has no row
// for the pair falls back to the Go registries' declared format strings
// (ResolveTranslator). The fallback is what keeps a deployment whose database
// predates a translator working; it is never allowed to override a database
// row. Nothing here writes to the database, and the seeded formats are not
// edited: the database is the fixed 0.5.x contract.

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/model"
)

// ResolutionSource says which side of the rule produced a translator name, so
// a caller can log (and a test can assert) that the database answered rather
// than the Go fallback.
type ResolutionSource string

const (
	// SourceDatabase is p_ResolveTranslationPlugin's answer: a tTranslation row
	// whose inputFormat is the render plugin's declared output format and whose
	// outputFormat the transport plugin's declared input formats include.
	SourceDatabase ResolutionSource = "database"

	// SourceGoRegistry is the fallback: no tTranslation row exists for the
	// pair, so the Go registries' own declared formats were used.
	SourceGoRegistry ResolutionSource = "go-registry"
)

// The database seams, as package variables so the unit tests can state the
// resolution rule with no MySQL running. Every one of them defaults to the
// model function that answers the same question from the real database.
var (
	lookupRenderPluginOutputFormat   = model.RenderPluginOutputFormat
	lookupTransportPluginInputFormat = model.TransportPluginInputFormat
	lookupTranslationsByInputFormat  = model.GetTranslationsByInputFormat
	lookupStylesheetOutputFormat     = StylesheetOutputFormat
)

// FormatInSet reports whether format is one of the comma-separated tokens in
// list - MySQL's FIND_IN_SET(format, list) > 0, which is the test
// p_ResolveTranslationPlugin applies to a tTranslation row's outputFormat
// (migrations/001_legacy.up.sql:336).
//
// MySQL's semantics are reproduced rather than approximated, because the
// comparison is the difference between resolving and failing:
//
//   - the list is split on ',' only; an element is not trimmed, and a space is
//     part of the element (FIND_IN_SET does not trim either);
//   - the comparison is case-insensitive, matching the collation every one of
//     these columns actually carries - the live schema's tTranslation,
//     tPlugins and tPluginOptions are utf8mb4_0900_ai_ci
//     (information_schema.TABLES.TABLE_COLLATION), so MySQL itself would match
//     'TEXT' to 'text'. It is accent-insensitive too; these values are ASCII
//     interchange formats, so that only matters for a value invented later.
//     A BINARY/trailing-space comparison would drop matches MySQL finds.
//   - an empty format or an empty list never matches: FIND_IN_SET(”, ”) is 0,
//     and FIND_IN_SET(str, ”) is 0 for every str.
//
// It deliberately does NOT reproduce MySQL's NULL propagation
// (FIND_IN_SET(NULL, list) is NULL, i.e. "no match"): an empty list is the
// absent case here, and both are false.
func FormatInSet(format string, list string) bool {
	if format == "" || list == "" {
		return false
	}
	for _, element := range strings.Split(list, ",") {
		if element != "" && strings.EqualFold(element, format) {
			return true
		}
	}
	return false
}

// ResolveTranslatorForJob resolves the translation plugin for one job the way
// the Java pipeline did, and reports which side of the rule answered.
//
//	renderPlugin        tPayload.renderPlugin (the Java FQCN)
//	renderOption        tPayload.renderOption (e.g. '4010_837p', 'statement')
//	transportPlugin     tPayload.transportPlugin (the Java FQCN)
//	transportOption     tPayload.transportOption (may be empty)
//	transportInputFormat the instantiated transport's own InputFormat(), used
//	                     only by the fallback
//
// The returned name is what InstantiateTranslator accepts: the tTranslation row
// stores the Java FQCN and the registry answers to both the FQCN and the short
// name (map.go), so either form instantiates.
//
// The Java's behaviour when nothing resolves is preserved: Java's
// resolveTranslationPlugin returned null for an unsatisfiable pair and
// RenderProcessorThread marked that payload failed rather than letting a null
// plugin through (RenderProcessorThread.java:100-118, "Translation plugin
// unavailable, setting payload as failed."). Here that is a non-nil error, and
// jobqueue's executeJob calls w.Fail with it, which journals
// tPayload.payloadState='failed'.
func ResolveTranslatorForJob(renderPlugin string, renderOption string, transportPlugin string, transportOption string, transportInputFormat string) (string, ResolutionSource, error) {
	renderFormat, err := lookupRenderPluginOutputFormat(renderPlugin, renderOption)
	if err != nil {
		return "", "", fmt.Errorf("translation: resolve: renderPluginOutputFormat(%q, %q): %w", renderPlugin, renderOption, err)
	}
	transportFormats, err := lookupTransportPluginInputFormat(transportPlugin, transportOption)
	if err != nil {
		return "", "", fmt.Errorf("translation: resolve: transportPluginInputFormat(%q, %q): %w", transportPlugin, transportOption, err)
	}

	// 'various' is the database's sentinel meaning "the format depends on the
	// option", not a format: no tTranslation row can carry it as an
	// inputFormat (and none does). model returns it verbatim for an option that
	// has no tPluginOptions row, which is what the stored function returns
	// (MySQL's SELECT INTO leaves its target alone), so it is flattened here
	// rather than looked up.
	renderSentinel := renderFormat == model.PluginFormatVarious
	transportSentinel := transportFormats == model.PluginFormatVarious
	if renderSentinel {
		renderFormat = ""
	}
	if transportSentinel {
		transportFormats = ""
	}

	// ---------------------------------------------------------------- database
	// p_ResolveTranslationPlugin's predicate, per candidate row. The rows are
	// the tTranslation rows for the render side's declared output format; the
	// membership test is the second half of the predicate.
	var notes []string
	var rowFormats []string
	if renderFormat != "" && transportFormats != "" {
		rows, err := lookupTranslationsByInputFormat(renderFormat)
		if err != nil {
			return "", "", fmt.Errorf("translation: resolve: tTranslation rows for inputFormat %q: %w", renderFormat, err)
		}
		for _, row := range rows {
			rowFormats = append(rowFormats, row.OutputFormat.String)
			if FormatInSet(row.OutputFormat.String, transportFormats) {
				return row.Plugin, SourceDatabase, nil
			}
		}
		if len(rows) > 0 {
			notes = append(notes, fmt.Sprintf("tTranslation has %d row(s) for inputFormat %q with outputFormat %s, none of which is among the transport's %q",
				len(rows), renderFormat, quoteList(rowFormats), transportFormats))
		} else {
			notes = append(notes, fmt.Sprintf("tTranslation has no row for inputFormat %q", renderFormat))
		}
	} else if renderFormat == "" {
		if renderSentinel {
			notes = append(notes, fmt.Sprintf("the database's tPlugins.outputFormat for render plugin %q is the 'various' sentinel and tPluginOptions has no row for option %q, so it declares no format", renderPlugin, renderOption))
		} else {
			notes = append(notes, fmt.Sprintf("the database declares no output format for render plugin %q option %q (tPlugins/tPluginOptions)", renderPlugin, renderOption))
		}
	} else {
		if transportSentinel {
			notes = append(notes, fmt.Sprintf("the database's tPlugins.inputFormat for transport plugin %q is the 'various' sentinel and tPluginOptions has no row for option %q, so it declares no format", transportPlugin, transportOption))
		} else {
			notes = append(notes, fmt.Sprintf("the database declares no input format for transport plugin %q option %q (tPlugins/tPluginOptions)", transportPlugin, transportOption))
		}
	}

	// ----------------------------------------------------------------- fallback
	// The Go registries' own declared formats, used only because the database
	// produced no translator for the pair. Candidates, in order:
	//
	//   1. the database's declared formats (kept first: a translator that
	//      matches them is still the database's answer, just not one named in
	//      tTranslation);
	//   2. the shipped stylesheet's own declaration of what it emits - the
	//      render plugin's declaration on the Go side, read from
	//      resources/xsl/<option>.xsl.xml (StylesheetOutputFormat);
	//   3. the render option itself, which is how the Go pipeline asked before
	//      this rule existed.
	//
	// and on the transport side the database's declared format first, then the
	// instantiated transport's InputFormat().
	inputs := appendUnique(nil, renderFormat)
	stylesheetFormat := lookupStylesheetOutputFormat(renderOption)
	if stylesheetFormat != "" {
		inputs = appendUnique(inputs, stylesheetFormat)
	}
	inputs = appendUnique(inputs, renderOption)

	outputs := appendUnique(nil, transportFormats)
	outputs = appendUnique(outputs, transportInputFormat)

	var tried []string
	for _, in := range inputs {
		for _, out := range outputs {
			tried = append(tried, fmt.Sprintf("%s->%s", in, out))
			if name, err := ResolveTranslator(in, out); err == nil {
				return name, SourceGoRegistry, nil
			}
		}
	}
	notes = append(notes, fmt.Sprintf("the Go fallback found no translator for %s (the Go registries' declared formats)", strings.Join(tried, ", ")))

	// The Java's null answer, with the reason attached.
	in := renderFormat
	if in == "" {
		in = renderOption
	}
	out := transportFormats
	if out == "" {
		out = transportInputFormat
	}
	return "", "", fmt.Errorf("unable to resolve translator between '%s' and '%s': %s", in, out, strings.Join(notes, "; "))
}

// appendUnique appends v unless it is empty or already present.
func appendUnique(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}

// quoteList renders formats for an error message: 'text' or 'text', 'pdf'.
func quoteList(vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = fmt.Sprintf("'%s'", v)
	}
	return strings.Join(q, ", ")
}

// pluginOptionDescriptor is the shipped stylesheet descriptor's shape - the
// legacy WEB-INF/xsl/<option>.xsl.xml files, copied into this tree as
// resources/xsl/<option>.xsl.xml:
//
//	<PluginOption>
//	  <PluginFile>statement.xsl</PluginFile>
//	  <Description>Patient Statements</Description>
//	  <InputFormat>statementxml</InputFormat>
//	  <OutputFormat>fixedformxml</OutputFormat>
//	  <Media>Paper</Media>
//	</PluginOption>
type pluginOptionDescriptor struct {
	XMLName      xml.Name `xml:"PluginOption"`
	PluginFile   string   `xml:"PluginFile"`
	Description  string   `xml:"Description"`
	InputFormat  string   `xml:"InputFormat"`
	OutputFormat string   `xml:"OutputFormat"`
	Media        string   `xml:"Media"`
}

// StylesheetOutputFormat returns the format the shipped stylesheet for a render
// option declares it emits (resources/xsl/<option>.xsl.xml, <OutputFormat>).
//
// This is the RENDER PLUGIN'S OWN DECLARATION ON THE GO SIDE, and it exists
// because the two sides of the database disagree about one shipped option:
//
//	tPluginOptions says  (XsltPlugin, 'statement') -> statementxml
//	statement.xsl.xml says                          -> fixedformxml
//
// 'statementxml' has no tTranslation row anywhere in the seeded table (and the
// stylesheet's output is fixed-form XML, which is what fixedformxml means), so
// resolving it from the database alone fails - as it did in Java, where
// RenderProcessorThread marked the payload failed
// (RenderProcessorThread.java:100-118) - while the stylesheet's own descriptor
// plus a tTranslation row for 'fixedformxml' resolves. The database is never
// edited to bridge that (it is the fixed 0.5.x contract), so the bridge is
// this: when, and only when, the database produces no translator, the Go side
// is allowed to say what its own stylesheet emits.
//
// An option with no descriptor, an unreadable one, or one that declares no
// outputFormat returns "" and the caller tries its next candidate: the fallback
// must never invent a format.
func StylesheetOutputFormat(option string) string {
	if option == "" {
		return ""
	}
	base := ""
	if config.Config != nil {
		base = config.Config.Paths.BasePath
	}
	if base == "" {
		return ""
	}
	path := filepath.Join(base, "resources", "xsl", option+".xsl.xml")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var d pluginOptionDescriptor
	if err := xml.Unmarshal(b, &d); err != nil {
		// A descriptor that does not parse declares nothing. The render stage
		// itself reports a stylesheet it cannot apply; this fallback only needs
		// to stay silent rather than guess.
		return ""
	}
	return strings.TrimSpace(d.OutputFormat)
}
