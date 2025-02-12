package translation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	//"fmt"
	"log"
	"os"
	"time"

	"github.com/freemed/remitt-server/model"
	"github.com/orcaman/writerseeker"
	"github.com/phpdave11/gofpdf"
	"github.com/phpdave11/gofpdf/contrib/gofpdi"
)

func init() {
	RegisterTranslator("fixedformpdf", func() Translator { return &TranslateFixedFormPDF{} })
}

type TranslateFixedFormPDF struct {
	TemplatePath string
	Debug        bool
	Benchmark    bool
	ctx          context.Context
}

func (t *TranslateFixedFormPDF) Resolver(in string, out string) bool {
	return (in == "fixedformxml" && out == "pdf") || (in == "fixedformxml" && out == "*")
}

func (t *TranslateFixedFormPDF) Translate(source any) (out []byte, err error) {
	st := time.Now()

	if t.Debug {
		log.Printf("Translate()")
	}

	src, ok := source.(model.FixedFormXml)
	if !ok {
		err = errors.New("invalid datatype presented")
		return
	}

	if t.Benchmark {
		log.Printf("Conversion : %s", time.Since(st).String())
	}

	// Create new PDF factory
	c := gofpdf.New(gofpdf.OrientationPortrait, "pt", "Letter", "courier")

	// Turn off auto page breaking, otherwise we will constantly be
	// interpolating additional page breaks in, creating mostly
	// blank pages.
	c.SetAutoPageBreak(false, 0)

	for iter := range src.Pages {
		err = t.RenderPage(c, src.Pages[iter])
		if err != nil {
			log.Printf("Translate(): %s", err.Error())
			return
		}
		if t.Benchmark {
			log.Printf("Page %d : %s", iter+1, time.Since(st).String())
		}
	}
	writerSeeker := &writerseeker.WriterSeeker{}
	err = c.OutputAndClose(writerSeeker)
	if err == nil {
		reader := writerSeeker.Reader()
		buf := new(bytes.Buffer)
		buf.ReadFrom(reader)
		out = buf.Bytes()
	}
	return
}

func (t *TranslateFixedFormPDF) SetContext(ctx context.Context) error {
	t.ctx = ctx
	return nil
}

func (t *TranslateFixedFormPDF) RenderPage(c *gofpdf.Fpdf, pageObj model.FixedFormPage) (err error) {
	if t.Debug {
		log.Printf("RenderPage()")
	}

	// Start by creating a new page
	c.AddPage()
	w, h := c.GetPageSize()
	log.Printf("TranslateFixedFormPDF.RenderPage(): w = %f, h = %f", w, h)

	if pageObj.Format.Pdf.Template != "" {
		f, err := os.Open(t.TemplatePath + string(os.PathSeparator) + pageObj.Format.Pdf.Template + ".pdf")
		if err != nil {
			return err
		}
		defer f.Close()

		// Import in-memory PDF stream with gofpdi free pdf document importer
		pdfReader := gofpdi.NewImporter()
		rs := io.ReadSeeker(f)
		templatePage := pdfReader.ImportPageFromStream(c, &rs, pageObj.Format.Pdf.Page, "/MediaBox")

		// Import/add page
		pdfReader.UseImportedTemplate(c, templatePage, 0, 0, w, h)
	}

	for iter := range pageObj.Elements {
		err = t.RenderElement(c, pageObj, pageObj.Elements[iter])
		if err != nil {
			log.Printf("TranslateFixedFormPDF.RenderPage(): ERR: %s", err.Error())
			return err
		}
	}

	return nil
}

func (t *TranslateFixedFormPDF) RenderElement(c *gofpdf.Fpdf, pageObj model.FixedFormPage, element model.FixedElement) (err error) {
	st := time.Now()

	if t.Debug {
		log.Printf("RenderElement(%#v)", element)
	}

	content := element.Content
	if element.OmitPdf {
		return nil
	}
	if element.PeriodStripPdf {
		content = strings.ReplaceAll(content, ".", " ")
	}

	// Cut if necessary to avoid overruns
	if len(content) > element.Length {
		content = content[0:element.Length]
	}

	if t.Benchmark {
		log.Printf("-- RenderElement(): NewParagraph: %s", time.Since(st).String())
	}

	// Column / X
	xPos := float64((float64(element.Column) * pageObj.Format.Pdf.Scaling.Horizontal) + pageObj.Format.Pdf.Offset.Horizontal)
	// Row / Y
	yPos := float64((float64(element.Row) * pageObj.Format.Pdf.Scaling.Vertical) + pageObj.Format.Pdf.Offset.Vertical)

	fontSt := time.Now()

	//p.SetFont(fonts.NewFontCourier())
	c.SetFont("Courier", "", pageObj.Format.Pdf.Font.Size)
	c.Text(xPos, yPos, content)
	log.Printf("RenderElement(): xPos: %f, yPos: %f, content: '%s'", xPos, yPos, content)

	if t.Benchmark {
		log.Printf("-- RenderElement(): SetFont/Pos: %s", time.Since(fontSt).String())
	}
	// Push to current page
	drawSt := time.Now()
	c.Cell(xPos, yPos, content)
	if t.Benchmark {
		log.Printf("-- RenderElement(): Cell: %s", time.Since(drawSt).String())
	}
	return
}

func (t *TranslateFixedFormPDF) RightPad(text string, length int) string {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x += " "
	}
	return x
}

func (t *TranslateFixedFormPDF) LeftZeroPad(text string, length int) string {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x = "0" + x
	}
	return x
}
