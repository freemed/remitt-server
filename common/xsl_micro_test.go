package common

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// xsl_micro_test.go — permanent regression net for the in-process ratago XSLT
// engine (github.com/freemed/ratago/xslt + the forked github.com/freemed/xpath).
//
// Why this file exists:
//
//	The production XSL transforms in REMITT are executed by XslTransformInternal
//	(the pure-Go ratago engine), but the historical reference implementation is
//	the external `xsltproc` binary. XslTransformExternal still exists as the
//	fallback. This file pins, one construct at a time, exactly what the
//	in-process engine is expected to produce, so that any change to ratago /
//	xpath / gokogiri that fixes (or breaks) a construct is caught immediately
//	and by name.
//
// KNOWN BROKEN today:
//
//	The cases flagged knownBroken in the table below are the exact XSLT
//	constructs where the in-process engine still differs from xsltproc:
//
//	  for_each_nodeset_var, with_param_nodeset, set_distinct,
//	  set_distinct_plain_nodeset, set_distinct_nodeset_var,
//	  predicate_var_compare, global_nodeset_var
//
//	They are asserted strictly on purpose — they are meant to FAIL until the
//	engine is fixed. Do not delete them, do not weaken them, do not flip them
//	to skips: they are the exit criteria. Once every case in this file passes,
//	the in-process engine is behaviourally equivalent (for these constructs) to
//	xsltproc and is ready to replace the xsltproc binary in the default path.
//
// The end-to-end comparison against the real xsltproc binary lives in
// xsl_test.go (TestXslTransform_Compare); that test needs the binary present
// and diffs whole real-world stylesheets. This file needs no external tools:
// it is hermetic, fast, and runs everywhere.
//
// Assertion strategy: run the transform, read the produced file, normalize it
// (drop any XML declaration, collapse whitespace runs to a single space, trim,
// drop whitespace between adjacent tags), then compare. Exact equality against
// the expected string is preferred; an output that merely *contains* the
// expected fragment is reported as a looser match so the difference stays
// visible in the log.

const microInputXML = `<remitt><practice id="1"><name>A</name></practice><practice id="2"><name>B</name></practice></remitt>`

type microCase struct {
	name string
	xsl  string
	// expected is the normalized output with the XML declaration stripped.
	expected string
	// knownBroken documents that this construct still differs from xsltproc.
	// It is used only for reporting; the assertion is strict either way.
	knownBroken bool
}

var microCases = []microCase{
	{
		name: "literal_only",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: literal_only -->
<xsl:template match="/remitt"><out>STATIC</out></xsl:template>
</xsl:stylesheet>`,
		expected: `<out>STATIC</out>`,
	},
	{
		name: "value_of_literal_path",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: value_of_literal_path -->
<xsl:template match="/remitt"><out><xsl:value-of select="count(//practice)"/></out></xsl:template>
</xsl:stylesheet>`,
		expected: `<out>2</out>`,
	},
	{
		name: "for_each_literal_path",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: for_each_literal_path -->
<xsl:template match="/remitt"><out><xsl:for-each select="//practice"><p><xsl:value-of select="@id"/></p></xsl:for-each></out></xsl:template>
</xsl:stylesheet>`,
		expected: `<out><p>1</p><p>2</p></out>`,
	},
	{
		name: "for_each_nodeset_var",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: for_each_nodeset_var -->
<xsl:template match="/remitt"><out><xsl:variable name="practices" select="//practice"/><xsl:for-each select="$practices"><p><xsl:value-of select="@id"/></p></xsl:for-each></out></xsl:template>
</xsl:stylesheet>`,
		expected:    `<out><p>1</p><p>2</p></out>`,
		knownBroken: true,
	},
	{
		name: "value_of_nodeset_var_count",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: value_of_nodeset_var_count -->
<xsl:template match="/remitt"><out><xsl:variable name="practices" select="//practice"/><xsl:value-of select="count($practices)"/></out></xsl:template>
</xsl:stylesheet>`,
		expected: `<out>2</out>`,
	},
	{
		name: "with_param_nodeset",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: with_param_nodeset -->
<xsl:template match="/remitt"><out><xsl:call-template name="t"><xsl:with-param name="ns" select="//practice"/></xsl:call-template></out></xsl:template>
<xsl:template name="t"><xsl:param name="ns"/><xsl:for-each select="$ns"><p><xsl:value-of select="@id"/></p></xsl:for-each></xsl:template>
</xsl:stylesheet>`,
		expected:    `<out><p>1</p><p>2</p></out>`,
		knownBroken: true,
	},
	{
		name: "param_from_go",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: param_from_go -->
<xsl:param name="jobId"/>
<xsl:template match="/remitt"><out><xsl:value-of select="$jobId"/></out></xsl:template>
</xsl:stylesheet>`,
		expected: `<out>JOB42</out>`,
	},
	{
		name: "set_distinct",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform" xmlns:set="http://exslt.org/sets" xmlns:exsl="http://exslt.org/common" exclude-result-prefixes="set exsl">
<!-- case: set_distinct -->
<xsl:template match="/remitt"><out><xsl:for-each select="set:distinct(//practice/@id)"><p><xsl:value-of select="."/></p></xsl:for-each></out></xsl:template>
</xsl:stylesheet>`,
		expected:    `<out><p>1</p><p>2</p></out>`,
		knownBroken: true,
	},
	{
		name: "set_distinct_plain_nodeset",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform" xmlns:set="http://exslt.org/sets" exclude-result-prefixes="set">
<!-- case: set_distinct_plain_nodeset -->
<xsl:template match="/remitt"><out><xsl:for-each select="set:distinct(//practice)"><p><xsl:value-of select="@id"/></p></xsl:for-each></out></xsl:template>
</xsl:stylesheet>`,
		expected:    `<out><p>1</p><p>2</p></out>`,
		knownBroken: true,
	},
	{
		name: "set_distinct_nodeset_var",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform" xmlns:set="http://exslt.org/sets" xmlns:exsl="http://exslt.org/common" exclude-result-prefixes="set exsl">
<!-- case: set_distinct_nodeset_var -->
<xsl:template match="/remitt"><out><xsl:variable name="procs" select="//practice"/><xsl:for-each select="set:distinct(exsl:node-set($procs/@id))"><p><xsl:value-of select="."/></p></xsl:for-each></out></xsl:template>
</xsl:stylesheet>`,
		expected:    `<out><p>1</p><p>2</p></out>`,
		knownBroken: true,
	},
	{
		name: "predicate_var_compare",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: predicate_var_compare -->
<xsl:template match="/remitt"><out><xsl:variable name="want" select="'1'"/><xsl:for-each select="//practice[@id=$want]"><p><xsl:value-of select="@id"/></p></xsl:for-each></out></xsl:template>
</xsl:stylesheet>`,
		expected:    `<out><p>1</p></out>`,
		knownBroken: true,
	},
	{
		name: "string_var_value_of",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: string_var_value_of -->
<xsl:template match="/remitt"><out><xsl:variable name="x" select="'hello'"/><xsl:value-of select="$x"/></out></xsl:template>
</xsl:stylesheet>`,
		expected: `<out>hello</out>`,
	},
	{
		name: "predicate_literal_only",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: predicate_literal_only -->
<xsl:template match="/remitt"><out><xsl:for-each select="//practice[@id='1']"><p><xsl:value-of select="@id"/></p></xsl:for-each></out></xsl:template>
</xsl:stylesheet>`,
		expected: `<out><p>1</p></out>`,
	},
	{
		name: "global_nodeset_var",
		xsl: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
<!-- case: global_nodeset_var -->
<xsl:variable name="allp" select="//practice"/>
<xsl:template match="/remitt"><out><xsl:for-each select="$allp"><p><xsl:value-of select="@id"/></p></xsl:for-each></out></xsl:template>
</xsl:stylesheet>`,
		expected:    `<out><p>1</p><p>2</p></out>`,
		knownBroken: true,
	},
}

var (
	microXMLDeclRe   = regexp.MustCompile(`(?s)^\s*<\?xml.*?\?>`)
	microSpaceRe     = regexp.MustCompile(`\s+`)
	microGapRe       = regexp.MustCompile(`>\s+<`)
	microAfterTagRe  = regexp.MustCompile(`>\s+`)
	microBeforeTagRe = regexp.MustCompile(`\s+<`)
)

// normalizeMicroOutput collapses insignificant whitespace so that indented and
// compact serializations of the same result compare equal. ratago serializes
// with IndentOutput, which wraps even text-only content in newlines and
// indentation (e.g. "<out>\n  STATIC\n</out>"), so whitespace adjacent to a
// tag boundary is dropped as well as collapsed. None of the cases here have
// mixed content, so no significant whitespace is at risk.
func normalizeMicroOutput(s string) string {
	s = microXMLDeclRe.ReplaceAllString(s, "")
	s = microSpaceRe.ReplaceAllString(s, " ")
	for microGapRe.MatchString(s) {
		s = microGapRe.ReplaceAllString(s, "><")
	}
	for microAfterTagRe.MatchString(s) {
		s = microAfterTagRe.ReplaceAllString(s, ">")
	}
	for microBeforeTagRe.MatchString(s) {
		s = microBeforeTagRe.ReplaceAllString(s, "<")
	}
	return strings.TrimSpace(s)
}

// TestXslMicro is the per-construct regression net described in the file header.
func TestXslMicro(t *testing.T) {
	dir := t.TempDir()

	inPath := filepath.Join(dir, "micro_in.xml")
	if err := os.WriteFile(inPath, []byte(microInputXML), 0o644); err != nil {
		t.Fatal(err)
	}

	params := map[string]string{"jobId": "JOB42"}

	for _, tc := range microCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			xslPath := filepath.Join(dir, tc.name+".xsl")
			if err := os.WriteFile(xslPath, []byte(tc.xsl), 0o644); err != nil {
				t.Fatal(err)
			}
			outPath := filepath.Join(dir, tc.name+".out.xml")
			_ = os.Remove(outPath)

			err := XslTransformInternal(inPath, xslPath, outPath, params)

			var raw string
			if b, readErr := os.ReadFile(outPath); readErr == nil {
				raw = string(b)
			}
			got := normalizeMicroOutput(raw)

			if err != nil {
				t.Errorf("XslTransformInternal error: %v\n  raw output: %q\n  normalized: %q\n  expected:   %q",
					err, raw, got, tc.expected)
				return
			}

			switch {
			case got == tc.expected:
				t.Logf("exact match: %s", got)
			case strings.Contains(got, tc.expected):
				t.Errorf("loose match: expected fragment is present but the output is not exactly equal\n  normalized: %s\n  expected:   %s", got, tc.expected)
			default:
				t.Errorf("output mismatch\n  normalized: %s\n  expected:   %s", got, tc.expected)
			}
		})
	}
}
