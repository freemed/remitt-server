package common

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/freemed/gokogiri/xml"
	"github.com/freemed/ratago/xslt"
	"github.com/freemed/remitt-server/config"
)

// readXMLFile reads an XML file and returns a parsed document.
// Strips the XML declaration (<?xml ...?>) and leading comment blocks
// to work around a pure-Go gokogiri parser bug where these cause nil Root().
func readXMLFile(path string) (*xml.XmlDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimSpace(data)
	// Strip XML declaration if present
	if bytes.HasPrefix(data, []byte("<?xml")) {
		if idx := bytes.Index(data, []byte("?>")); idx >= 0 {
			data = bytes.TrimSpace(data[idx+2:])
		}
	}
	// Strip leading comment blocks
	for bytes.HasPrefix(data, []byte("<!--")) {
		if idx := bytes.Index(data, []byte("-->") ); idx >= 0 {
			data = bytes.TrimSpace(data[idx+3:])
		} else {
			break
		}
	}
	return xml.Parse(data, xml.DefaultEncodingBytes, []byte(path),
		xml.DefaultParseOption, xml.DefaultEncodingBytes)
}

// XslTransform performs an XSL transform with the engine the configuration
// selects.
//
// The in-process engine is the default (internal-xslt: true). It produces
// output byte-identical to xsltproc for the shipped stylesheets, but xsltproc
// is still kept in the image as a fallback: if the in-process engine returns an
// error, the external binary is tried before the render is failed, because a
// document produced by the fallback is worth more to the pipeline than no
// document at all. The fallback is skipped when no binary path is configured,
// so an unset path reports the real cause instead of "exec: no command".
func XslTransform(inxml, xslfile, outxml string, vars map[string]string) error {
	if !config.Config.InternalXslt {
		return XslTransformExternal(inxml, xslfile, outxml, vars)
	}

	err := XslTransformInternal(inxml, xslfile, outxml, vars)
	if err == nil {
		return nil
	}
	if config.Config.Paths.XsltProcPath == "" {
		return err
	}

	log.Printf("XslTransform: in-process engine failed, falling back to %q: %v",
		config.Config.Paths.XsltProcPath, err)
	if fallbackErr := XslTransformExternal(inxml, xslfile, outxml, vars); fallbackErr != nil {
		return fmt.Errorf("in-process engine failed (%v) and %s fallback failed (%v)",
			err, config.Config.Paths.XsltProcPath, fallbackErr)
	}
	return nil
}

// XslTransformInternal uses the ratago native Go XSL implementation to perform XSL
// transforms with parameters.
func XslTransformInternal(inxml, xslfile, outxml string, vars map[string]string) error {
	log.Printf("XslTransform(): %v", vars)

	style, err := readXMLFile(xslfile)
	if err != nil {
		return err
	}

	doc, err := readXMLFile(inxml)
	if err != nil {
		return err
	}

	stylesheet, err := xslt.ParseStylesheet(style, xslfile)
	if err != nil {
		return err
	}

	params := map[string]any{}
	for k, v := range vars {
		params[k] = v
	}
	output, err := stylesheet.Process(doc, xslt.StylesheetOptions{IndentOutput: true, Parameters: params})
	if err != nil {
		return err
	}

	return os.WriteFile(outxml, []byte(output), 0644)
}

// XslTransformExternal uses the xsltproc binary to perform XSL transforms
// with parameters.
func XslTransformExternal(inxml, xslfile, outxml string, vars map[string]string) error {
	log.Printf("XslTransformExternal(): %v", vars)

	args := []string{config.Config.Paths.XsltProcPath}
	for k, v := range vars {
		args = append(args, "--stringparam")
		args = append(args, k)
		args = append(args, v)
	}
	args = append(args, "-o")
	args = append(args, outxml)
	args = append(args, xslfile)
	args = append(args, inxml)

	log.Printf("XslTransformExternal(): %s", strings.Join(args, " "))

	_, err := RunWithTimeout(args, 30)
	if err != nil {
		return err
	}
	return err
}
