package model

import (
	"context"
	"database/sql"
	"errors"

	"github.com/freemed/remitt-server/internal/dbgen"
)

type TranslationModel struct {
	Plugin       string     `db:"plugin"`
	InputFormat  NullString `db:"inputFormat"`
	OutputFormat NullString `db:"outputFormat"`
}

// PluginFormatVarious is the sentinel the legacy database stores in
// tPlugins.outputFormat / tPlugins.inputFormat for a plugin whose format is not
// one value: 'various' means "ask tPluginOptions for the option". The stored
// functions renderPluginOutputFormat and transportPluginInputFormat both branch
// on it (migrations/001_legacy.up.sql:286, :303), and
// org.remitt.plugin.render.XsltPlugin is the row that carries it (its output
// depends on the stylesheet, so the per-option tPluginOptions row is the only
// place the answer exists).
const PluginFormatVarious = "various"

// RenderPluginOutputFormat is renderPluginOutputFormat(pluginClass,
// pluginOption) from the legacy schema (migrations/001_legacy.up.sql:276-291),
// reimplemented against the same tables in Go:
//
//	SELECT outputFormat FROM tPlugins WHERE plugin = pluginClass;
//	IF ret = 'various' THEN
//	  SELECT outputFormat FROM tPluginOptions WHERE plugin = pluginClass AND poption = pluginOption;
//	END IF;
//
// An unknown plugin class is the NULL those functions return, and is reported
// as an empty string with a nil error; an option with no tPluginOptions row
// comes back as the plugin's own 'various', because MySQL's SELECT ... INTO
// leaves its target untouched when the query matches nothing (measured live:
// renderPluginOutputFormat('org.remitt.plugin.render.XsltPlugin','nosuchoption')
// returns 'various'). Either way the caller decides what "the database has no
// declared format" means; translation.ResolveTranslatorForJob treats both the
// empty string and the 'various' sentinel as "no format", and falls back to the
// Go registries' own declarations.
//
// The format is returned verbatim, as the stored function returns it: no
// trimming, no case folding.
func RenderPluginOutputFormat(pluginClass string, pluginOption string) (string, error) {
	return pluginDeclaredFormat(pluginClass, pluginOption, true)
}

// TransportPluginInputFormat is transportPluginInputFormat(pluginClass,
// pluginOption) from the legacy schema (migrations/001_legacy.up.sql:293-310):
// the same shape as RenderPluginOutputFormat, reading inputFormat instead of
// outputFormat. It is the format list a transport plugin accepts, which
// p_ResolveTranslationPlugin hands to FIND_IN_SET.
func TransportPluginInputFormat(pluginClass string, pluginOption string) (string, error) {
	return pluginDeclaredFormat(pluginClass, pluginOption, false)
}

// pluginDeclaredFormat is the shared body of the two functions above: the
// plugin's own row, then the per-option row when that value is 'various'.
func pluginDeclaredFormat(pluginClass string, pluginOption string, output bool) (string, error) {
	if Queries == nil {
		return "", errors.New("model: database is not initialised")
	}
	ctx := context.Background()

	row, err := Queries.GetPluginByPluginName(ctx, pluginClass)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// No such plugin class: the stored function's SELECT INTO leaves ret
		// NULL and it returns NULL.
		return "", nil
	case err != nil:
		return "", err
	}

	format := pluginFormatValue(row.Inputformat, row.Outputformat, output)
	if format == "" || format != PluginFormatVarious {
		return format, nil
	}

	opt, err := Queries.GetPluginOptionByPluginAndPoption(ctx, dbgen.GetPluginOptionByPluginAndPoptionParams{
		Plugin:  pluginClass,
		Poption: pluginOption,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// MySQL's SELECT ... INTO does NOT assign when the query returns no
		// row - the variable keeps the value it already had. So the stored
		// function returns the plugin's own 'various' for an option that has
		// no tPluginOptions row, not NULL. Measured live:
		//   SELECT renderPluginOutputFormat('org.remitt.plugin.render.XsltPlugin','nosuchoption')
		//   -> 'various'
		// The difference matters: 'various' is a sentinel, not a format, so the
		// caller must treat it as "no format" rather than look it up (see
		// translation.ResolveTranslatorForJob).
		return format, nil
	case err != nil:
		return "", err
	}
	return pluginFormatValue(opt.Inputformat, opt.Outputformat, output), nil
}

// pluginFormatValue picks the column and flattens the NULL case to "".
func pluginFormatValue(input sql.NullString, output sql.NullString, wantOutput bool) string {
	v := input
	if wantOutput {
		v = output
	}
	if !v.Valid {
		return ""
	}
	return v.String
}

// GetTranslationsByInputFormat returns the tTranslation rows whose inputFormat
// is the given format - the candidate translators for a render plugin that
// declares that output format (the first half of p_ResolveTranslationPlugin's
// predicate, migrations/001_legacy.up.sql:334-337). The rows come back ordered
// by plugin.
func GetTranslationsByInputFormat(inputFormat string) ([]TranslationModel, error) {
	if Queries == nil {
		return nil, errors.New("model: database is not initialised")
	}
	rows, err := Queries.GetTranslationsByInputFormat(context.Background(), inputFormat)
	if err != nil {
		return nil, err
	}
	o := make([]TranslationModel, len(rows))
	for i, r := range rows {
		o[i] = TranslationModel{
			Plugin:       r.Plugin,
			InputFormat:  NewNullStringValue(r.Inputformat),
			OutputFormat: NewNullStringValue(r.Outputformat),
		}
	}
	return o, nil
}
