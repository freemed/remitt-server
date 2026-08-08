package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/config"
)

func init() {
	RegisterRenderer("org.remitt.plugin.render.XsltPlugin", func() Renderer { return &XsltPlugin{} })
}

// XsltPlugin applies an XSLT stylesheet to transform input XML.
type XsltPlugin struct {
	ctx context.Context
}

func (x *XsltPlugin) Render(input []byte, option string) ([]byte, error) {
	// Create temp input file
	inFile, err := os.CreateTemp(config.Config.Paths.TemporaryPath, "render-xslt-in")
	if err != nil {
		return nil, fmt.Errorf("xslt: create temp input: %w", err)
	}
	defer os.Remove(inFile.Name())
	if _, err := inFile.Write(input); err != nil {
		return nil, fmt.Errorf("xslt: write temp input: %w", err)
	}
	inFile.Close()

	// Create temp output file
	outFile, err := os.CreateTemp(config.Config.Paths.TemporaryPath, "render-xslt-out")
	if err != nil {
		return nil, fmt.Errorf("xslt: create temp output: %w", err)
	}
	defer os.Remove(outFile.Name())
	outFile.Close()

	// Resolve XSL file
	xslFile := filepath.Join(config.Config.Paths.BasePath, "resources", "xsl", option+".xsl")

	// Apply transform
	if config.Config.InternalXslt {
		err = common.XslTransformInternal(inFile.Name(), xslFile, outFile.Name(), map[string]string{})
	} else {
		err = common.XslTransformExternal(inFile.Name(), xslFile, outFile.Name(), map[string]string{})
	}
	if err != nil {
		return nil, fmt.Errorf("xslt: transform: %w", err)
	}

	// Read result
	return os.ReadFile(outFile.Name())
}

func (x *XsltPlugin) SetContext(ctx context.Context) error {
	x.ctx = ctx
	return nil
}
