package common

import (
	"github.com/ThomsonReutersEikon/gokogiri/xml"
	"github.com/freemed/remitt-server/config"
	"github.com/jbowtie/ratago/xslt"
	"io/ioutil"
	"log"
	"strings"
)

// XslTransform uses the ratago native Go XSL implementation to perform XSL
// transforms with parameters.
func XslTransform(inxml, xslfile, outxml string, vars map[string]string) error {
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

	params := map[string]interface{}{}
	for k, v := range vars {
		params[k] = v
	}
	output, err := stylesheet.Process(doc, xslt.StylesheetOptions{true, params})
	if err != nil {
		return err
	}

	return ioutil.WriteFile(outxml, []byte(output), 644)
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

	log.Printf("XslTransformExternal(): " + strings.Join(args, " "))

	_, err := RunWithTimeout(args, 5)
	if err != nil {
		return err
	}
	return err
}
