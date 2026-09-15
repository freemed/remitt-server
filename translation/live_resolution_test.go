package translation

// live_resolution_test.go is the LIVE verification of the translation
// resolution rule (resolve.go) against the provisioned MySQL, exactly the way
// translation/registry_live_test.go verifies the registry: it reads what the
// database actually stores and what the database's OWN stored functions and
// procedure answer, and asserts the Go resolution agrees with them.
//
// It is skipped unless REMITT_LIVE_CONFIG points at a remitt-server
// configuration file whose database is reachable:
//
//	REMITT_LIVE_CONFIG=/tmp/remitt-e2e.yml go test -count=1 -v \
//	    -run TestLive_TranslationResolution ./...
//
// What it pins, per case, with the values quoted from the live rows:
//
//   - model.RenderPluginOutputFormat and model.TransportPluginInputFormat agree,
//     value for value, with the database's own renderPluginOutputFormat and
//     transportPluginInputFormat (migrations/001_legacy.up.sql:276-310). This is
//     the equivalence check: the Go port of the stored functions against the
//     stored functions themselves.
//   - when CALL p_ResolveTranslationPlugin(...) names a plugin, the Go
//     resolution returns that same plugin and says the database answered it.
//   - when the procedure names nothing, the Go resolution either reports the
//     Go-registry fallback (and which plugin it named) or fails with an error
//     naming both formats and why nothing was found - the Java's null answer,
//     which RenderProcessorThread.java:100-118 turned into a failed payload.
//   - the four shipped stylesheet options (4010_837p, 5010_837p, cms1500,
//     statement) each resolve.
//
// Nothing is written to the database: the file is read-only.

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/model"
)

const (
	liveXslt        = "org.remitt.plugin.render.XsltPlugin"
	livePreRendered = "org.remitt.plugin.render.PreRenderedPlugin"

	liveStoreFile        = "org.remitt.plugin.transport.StoreFile"
	liveStoreFilePdf     = "org.remitt.plugin.transport.StoreFilePdf"
	liveSftp             = "org.remitt.plugin.transport.SftpTransport"
	liveScriptedHttp     = "org.remitt.plugin.transport.ScriptedHttpTransport"
	liveGatewayEdi       = "org.remitt.plugin.transport.GatewayEdiTransport"
	liveNoSuchTransport  = "org.remitt.plugin.transport.NoSuchTransport"
)

// goTransportInputFormat is the format the Go transport registry declares for
// each transport, which the fallback may use - NOT what the database declares
// (tPlugins.inputFormat is 'text' for every one of them, while the Go
// transports answer 'x12', 'pdf' or '*'; transport/map_test.go pins the Go
// side). The two vocabularies are exactly why the old resolution - which
// compared the render OPTION with this string - could never match.
var goTransportInputFormat = map[string]string{
	liveStoreFile:    "*",
	liveStoreFilePdf: "pdf",
	liveSftp:         "x12",
	liveScriptedHttp: "x12",
	liveGatewayEdi:   "x12",
}

func TestLive_TranslationResolution(t *testing.T) {
	db := liveTranslationDatabase(t)

	// ------------------------------------------------------- what is stored
	t.Log("tPlugins rows the resolution reads (plugin, category, inputFormat, outputFormat):")
	prows, err := db.Query(`SELECT plugin, category, inputFormat, outputFormat FROM tPlugins ORDER BY category, plugin`)
	if err != nil {
		t.Fatalf("select tPlugins: %v", err)
	}
	for prows.Next() {
		var plugin, category string
		var in, out sql.NullString
		if err := prows.Scan(&plugin, &category, &in, &out); err != nil {
			t.Fatalf("scan tPlugins: %v", err)
		}
		t.Logf("    plugin=%q category=%q inputFormat=%s outputFormat=%s",
			plugin, category, nullText(in), nullText(out))
	}
	if err := prows.Err(); err != nil {
		t.Fatalf("iterate tPlugins: %v", err)
	}
	prows.Close()

	t.Log("tPluginOptions rows (the per-option format of a 'various' plugin):")
	orows, err := db.Query(`SELECT plugin, poption, inputFormat, outputFormat FROM tPluginOptions ORDER BY plugin, poption`)
	if err != nil {
		t.Fatalf("select tPluginOptions: %v", err)
	}
	for orows.Next() {
		var plugin, poption string
		var in, out sql.NullString
		if err := orows.Scan(&plugin, &poption, &in, &out); err != nil {
			t.Fatalf("scan tPluginOptions: %v", err)
		}
		t.Logf("    plugin=%q poption=%q inputFormat=%s outputFormat=%s",
			plugin, poption, nullText(in), nullText(out))
	}
	if err := orows.Err(); err != nil {
		t.Fatalf("iterate tPluginOptions: %v", err)
	}
	orows.Close()

	t.Log("tTranslation rows (the translators the database names):")
	trows, err := db.Query(`SELECT plugin, inputFormat, outputFormat FROM tTranslation ORDER BY inputFormat, plugin`)
	if err != nil {
		t.Fatalf("select tTranslation: %v", err)
	}
	for trows.Next() {
		var plugin, in, out string
		if err := trows.Scan(&plugin, &in, &out); err != nil {
			t.Fatalf("scan tTranslation: %v", err)
		}
		t.Logf("    plugin=%q inputFormat=%q outputFormat=%q", plugin, in, out)
	}
	if err := trows.Err(); err != nil {
		t.Fatalf("iterate tTranslation: %v", err)
	}
	trows.Close()

	// --------------------------- the Go functions vs the stored functions
	t.Log("renderPluginOutputFormat / transportPluginInputFormat: Go (model) vs the database's own function:")
	pluginOptions := [][2]string{
		{liveXslt, "4010_837p"},
		{liveXslt, "5010_837p"},
		{liveXslt, "cms1500"},
		{liveXslt, "statement"},
		{liveXslt, "nosuchoption"},
		{livePreRendered, "x12"},
		{"org.remitt.plugin.render.NoSuchPlugin", "x"},
	}
	for _, po := range pluginOptions {
		want, err := storedFunctionValue(t, db,
			`SELECT renderPluginOutputFormat(?, ?)`, po[0], po[1])
		if err != nil {
			t.Fatalf("renderPluginOutputFormat(%q, %q): %v", po[0], po[1], err)
		}
		got, gerr := model.RenderPluginOutputFormat(po[0], po[1])
		if gerr != nil {
			t.Fatalf("model.RenderPluginOutputFormat(%q, %q) = %v", po[0], po[1], gerr)
		}
		t.Logf("    renderPluginOutputFormat(%q, %q): database=%q go=%q", po[0], po[1], want, got)
		if got != want {
			t.Errorf("renderPluginOutputFormat(%q, %q): database says %q, model says %q", po[0], po[1], want, got)
		}
		// The same equivalence for the transport side, with the option the
		// database stores (NULL for every seeded transport except the scripted
		// HTTP plugin's two).
		for _, opt := range []string{"", "ClaimLogic"} {
			wantT, err := storedFunctionValue(t, db,
				`SELECT transportPluginInputFormat(?, NULLIF(?, ''))`, po[0], opt)
			if err != nil {
				t.Fatalf("transportPluginInputFormat(%q, %q): %v", po[0], opt, err)
			}
			gotT, gerr := model.TransportPluginInputFormat(po[0], opt)
			if gerr != nil {
				t.Fatalf("model.TransportPluginInputFormat(%q, %q) = %v", po[0], opt, gerr)
			}
			t.Logf("    transportPluginInputFormat(%q, %q): database=%q go=%q", po[0], opt, wantT, gotT)
			if gotT != wantT {
				t.Errorf("transportPluginInputFormat(%q, %q): database says %q, model says %q", po[0], opt, wantT, gotT)
			}
		}
	}

	// ------------------------------------------------- the resolution rule
	type liveCase struct {
		name            string
		renderPlugin    string
		renderOption    string
		transportPlugin string
		transportOption string
		wantPlugin      string // the plugin the LIVE procedure names ("" = none)
		wantGoPlugin    string // what the Go resolution must answer ("" = must fail)
		wantSource      ResolutionSource
	}
	cases := []liveCase{
		{
			name: "4010_837p / storefile", renderPlugin: liveXslt, renderOption: "4010_837p",
			transportPlugin: liveStoreFile,
			wantPlugin:      "org.remitt.plugin.translation.X12Xml", wantGoPlugin: "org.remitt.plugin.translation.X12Xml", wantSource: SourceDatabase,
		},
		{
			name: "5010_837p / storefile", renderPlugin: liveXslt, renderOption: "5010_837p",
			transportPlugin: liveStoreFile,
			wantPlugin:      "org.remitt.plugin.translation.X12Xml", wantGoPlugin: "org.remitt.plugin.translation.X12Xml", wantSource: SourceDatabase,
		},
		{
			name: "cms1500 / storefile (text)", renderPlugin: liveXslt, renderOption: "cms1500",
			transportPlugin: liveStoreFile,
			wantPlugin:      "org.remitt.plugin.translation.FixedFormXml", wantGoPlugin: "org.remitt.plugin.translation.FixedFormXml", wantSource: SourceDatabase,
		},
		{
			name: "cms1500 / storefilepdf (pdf)", renderPlugin: liveXslt, renderOption: "cms1500",
			transportPlugin: liveStoreFilePdf,
			wantPlugin:      "org.remitt.plugin.translation.FixedFormPdf", wantGoPlugin: "org.remitt.plugin.translation.FixedFormPdf", wantSource: SourceDatabase,
		},
		{
			name: "4010_837p / sftp", renderPlugin: liveXslt, renderOption: "4010_837p",
			transportPlugin: liveSftp,
			wantPlugin:      "org.remitt.plugin.translation.X12Xml", wantGoPlugin: "org.remitt.plugin.translation.X12Xml", wantSource: SourceDatabase,
		},
		{
			name: "4010_837p / scripted http ClaimLogic (the Java unit test's case)",
			renderPlugin: liveXslt, renderOption: "4010_837p",
			transportPlugin: liveScriptedHttp, transportOption: "ClaimLogic",
			wantPlugin:      "org.remitt.plugin.translation.X12Xml", wantGoPlugin: "org.remitt.plugin.translation.X12Xml", wantSource: SourceDatabase,
		},
		{
			name: "4010_837p / gatewayedi", renderPlugin: liveXslt, renderOption: "4010_837p",
			transportPlugin: liveGatewayEdi,
			wantPlugin:      "org.remitt.plugin.translation.X12Xml", wantGoPlugin: "org.remitt.plugin.translation.X12Xml", wantSource: SourceDatabase,
		},
		{
			// No tTranslation row has inputFormat 'statementxml', so the
			// database names nothing; the shipped statement.xsl.xml declares the
			// stylesheet's own output as fixedformxml, which the Go registry
			// resolves to the fixed-form translator.
			name: "statement / storefile (the database names no translator)",
			renderPlugin: liveXslt, renderOption: "statement",
			transportPlugin: liveStoreFile,
			wantPlugin:      "", wantGoPlugin: "fixedformxml", wantSource: SourceGoRegistry,
		},
		{
			name: "statement / storefilepdf (the database names no translator)",
			renderPlugin: liveXslt, renderOption: "statement",
			transportPlugin: liveStoreFilePdf,
			wantPlugin:      "", wantGoPlugin: "fixedformpdf", wantSource: SourceGoRegistry,
		},
		{
			name: "statement / sftp (the database names no translator)",
			renderPlugin: liveXslt, renderOption: "statement",
			transportPlugin: liveSftp,
			wantPlugin:      "", wantGoPlugin: "fixedformxml", wantSource: SourceGoRegistry,
		},
		{
			// PreRenderedPlugin declares outputFormat 'x12' and tTranslation has
			// no row for it, while the x12 passthrough is registered on
			// 'x12' -> 'x12'/'*'.
			name: "PreRendered x12 / storefile", renderPlugin: livePreRendered, renderOption: "x12",
			transportPlugin: liveStoreFile,
			wantPlugin:      "", wantGoPlugin: "x12passthrough", wantSource: SourceGoRegistry,
		},
		{
			name: "PreRendered x12 / sftp", renderPlugin: livePreRendered, renderOption: "x12",
			transportPlugin: liveSftp,
			wantPlugin:      "", wantGoPlugin: "x12passthrough", wantSource: SourceGoRegistry,
		},
		{
			// x12xml cannot become a pdf: the database has the x12xml row, its
			// outputFormat 'text' is not among the transport's 'pdf', and no
			// registered translator does x12xml -> pdf.
			name: "4010_837p / storefilepdf (nothing can resolve)", renderPlugin: liveXslt, renderOption: "4010_837p",
			transportPlugin: liveStoreFilePdf,
			wantPlugin:      "", wantGoPlugin: "", wantSource: "",
		},
		{
			// A transport class the database does not store: no input format,
			// so there is not even a pair to look up.
			name: "4010_837p / a transport the database does not know",
			renderPlugin: liveXslt, renderOption: "4010_837p",
			transportPlugin: liveNoSuchTransport,
			wantPlugin:      "", wantGoPlugin: "", wantSource: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stored, err := storedProcedureAnswer(t, db,
				c.renderPlugin, c.renderOption, c.transportPlugin, c.transportOption)
			if err != nil {
				t.Fatalf("CALL p_ResolveTranslationPlugin(%q, %q, %q, %q): %v",
					c.renderPlugin, c.renderOption, c.transportPlugin, c.transportOption, err)
			}
			t.Logf("CALL p_ResolveTranslationPlugin('%s', '%s', '%s', '%s') -> %s",
				c.renderPlugin, c.renderOption, c.transportPlugin, c.transportOption, quoteOrNone(stored))
			if stored != c.wantPlugin {
				t.Errorf("the live procedure answered %s; the case says %s",
					quoteOrNone(stored), quoteOrNone(c.wantPlugin))
			}

			goIn := goTransportInputFormat[c.transportPlugin]
			name, source, rerr := ResolveTranslatorForJob(c.renderPlugin, c.renderOption,
				c.transportPlugin, c.transportOption, goIn)

			switch {
			case c.wantGoPlugin == "":
				if rerr == nil {
					t.Fatalf("ResolveTranslatorForJob(%q, %q, %q, %q, %q) = %q (%s); nothing can resolve between those formats",
						c.renderPlugin, c.renderOption, c.transportPlugin, c.transportOption, goIn, name, source)
				}
				t.Logf("ResolveTranslatorForJob -> error (as the Java's null did): %s", rerr.Error())
				if !strings.Contains(rerr.Error(), "unable to resolve translator between") {
					t.Errorf("the failure does not say what could not be resolved: %s", rerr.Error())
				}
			default:
				if rerr != nil {
					t.Fatalf("ResolveTranslatorForJob(%q, %q, %q, %q, %q) = %v; want %q",
						c.renderPlugin, c.renderOption, c.transportPlugin, c.transportOption, goIn, rerr, c.wantGoPlugin)
				}
				t.Logf("ResolveTranslatorForJob -> %q (%s)  [Go transport InputFormat() = %q]",
					name, source, goIn)
				if name != c.wantGoPlugin {
					t.Errorf("resolved %q; want %q", name, c.wantGoPlugin)
				}
				if source != c.wantSource {
					t.Errorf("resolved by %q; want %q", source, c.wantSource)
				}
				// Whatever the source, the name must instantiate: the database
				// stores Java FQCNs, the registry answers to those and to the
				// short names.
				if m, err := InstantiateTranslator(name); err != nil {
					t.Errorf("InstantiateTranslator(%q) [the resolved name]: %v", name, err)
				} else {
					t.Logf("    InstantiateTranslator(%q) -> %T", name, m)
				}
			}

			// When the database itself named a plugin, the Go resolution must
			// be that plugin - the whole point of the rule.
			if stored != "" && rerr == nil && name != stored {
				t.Errorf("the database named %q and the Go resolution answered %q", stored, name)
			}
		})
	}
}

// storedFunctionValue evaluates one of the schema's helper functions.
func storedFunctionValue(t *testing.T, db *sql.DB, query string, args ...any) (string, error) {
	t.Helper()
	var v sql.NullString
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		return "", err
	}
	if !v.Valid {
		return "", nil
	}
	return v.String, nil
}

// storedProcedureAnswer is what CALL p_ResolveTranslationPlugin answers for a
// (renderPlugin, renderOption, transportPlugin, transportOption) tuple: the
// plugin name, or "" when the procedure returns no row - which is the NULL the
// Java's Configuration.resolveTranslationPlugin read as "no translation plugin"
// (Configuration.java:229-262) and RenderProcessorThread marked the payload
// failed for.
func storedProcedureAnswer(t *testing.T, db *sql.DB, renderPlugin, renderOption, transportPlugin, transportOption string) (string, error) {
	t.Helper()
	rows, err := db.Query(`CALL p_ResolveTranslationPlugin(?, ?, ?, NULLIF(?, ''))`,
		renderPlugin, renderOption, transportPlugin, transportOption)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", rows.Err()
	}
	var plugin string
	if err := rows.Scan(&plugin); err != nil {
		return "", err
	}
	return plugin, nil
}

func nullText(v sql.NullString) string {
	if !v.Valid {
		return "NULL"
	}
	return fmt.Sprintf("%q", v.String)
}

func quoteOrNone(s string) string {
	if s == "" {
		return "<no row: no translator resolves>"
	}
	return fmt.Sprintf("%q", s)
}
