package common

import (
	//"io/ioutil"

	"log"
	"os"
	"strings"

	"github.com/freemed/gokogiri/xml"
	"github.com/freemed/ratago/xslt"
	"github.com/freemed/remitt-server/config"
)

// XslTransformIntermal uses the ratago native Go XSL implementation to perform XSL
// transforms with parameters.
func XslTransformInternal(inxml, xslfile, outxml string, vars map[string]string) error {
	log.Printf("XslTransform(): %v", vars)

	style, err := xml.ReadFile(xslfile, xml.StrictParseOption)
	if err != nil {
		return err
	}

	doc, err := xml.ReadFile(inxml, xml.StrictParseOption)
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
