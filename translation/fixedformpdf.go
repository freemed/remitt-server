package translation

import (
	"bytes"
	"errors"
	"strings"

	//"fmt"
	"log"
	"os"
	"time"

	"github.com/freemed/remitt-server/model"
	"github.com/orcaman/writerseeker"
	"github.com/unidoc/unidoc/pdf/creator"
	pdf "github.com/unidoc/unidoc/pdf/model"
	"github.com/unidoc/unidoc/pdf/model/fonts"
)

func init() {
	RegisterTranslator("fixedformpdf", func() Translator { return &TranslateFixedFormPDF{} })
}

type TranslateFixedFormPDF struct {
	TemplatePath string
	Debug        bool
	Benchmark    bool
}

func (t *TranslateFixedFormPDF) Resolver(in string, out string) bool {
	return (in == "fixedformxml" && out == "pdf") || (in == "fixedformxml" && out == "*")
}

func (t *TranslateFixedFormPDF) Translate(source interface{}) (out []byte, err error) {
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
		log.Printf("Conversion : %s", time.Now().Sub(st).String())
	}

	// Create new PDF factory with unidoc
	c := creator.New()
	for iter := range src.Pages {
		err = t.RenderPage(c, src.Pages[iter])
		if err != nil {
			return
		}
		if t.Benchmark {
			log.Printf("Page %d : %s", iter+1, time.Now().Sub(st).String())
		}
	}
	writerSeeker := &writerseeker.WriterSeeker{}
	err = c.Write(writerSeeker)
	if err == nil {
		reader := writerSeeker.Reader()
		buf := new(bytes.Buffer)
		buf.ReadFrom(reader)
		out = buf.Bytes()
	}
	return
}

func (t *TranslateFixedFormPDF) RenderPage(c *creator.Creator, pageObj model.FixedFormPage) (err error) {
	if t.Debug {
		log.Printf("RenderPage()")
	}
	if pageObj.Format.Pdf.Template != "" {
		f, err := os.Open(t.TemplatePath + string(os.PathSeparator) + pageObj.Format.Pdf.Template + ".pdf")
		if err != nil {
			return err
		}
		defer f.Close()

		pdfReader, err := pdf.NewPdfReader(f)
		if err != nil {
			return err
		}

		templatePage, err := pdfReader.GetPage(pageObj.Format.Pdf.Page)
		if err != nil {
			return err
		}

		err = c.AddPage(templatePage)
		if err != nil {
			return err
		}
	} else {
		c.NewPage()
	}

	for iter := range pageObj.Elements {
		err = t.RenderElement(c, pageObj, pageObj.Elements[iter])
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *TranslateFixedFormPDF) RenderElement(c *creator.Creator, pageObj model.FixedFormPage, element model.FixedElement) (err error) {
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

	p := creator.NewParagraph(content)
	if t.Benchmark {
		log.Printf("-- RenderElement(): NewParagraph: %s", time.Now().Sub(st).String())
	}

	// Column / X
	xPos := float64((float64(element.Column) * pageObj.Format.Pdf.Scaling.Horizontal) + pageObj.Format.Pdf.Offset.Horizontal)
	// Row / Y
	yPos := float64((float64(element.Row) * pageObj.Format.Pdf.Scaling.Vertical) + pageObj.Format.Pdf.Offset.Vertical)

	fontSt := time.Now()

	p.SetFont(fonts.NewFontCourier())
	p.SetFontSize(pageObj.Format.Pdf.Font.Size)
	p.SetPos(xPos, yPos)

	if t.Benchmark {
		log.Printf("-- RenderElement(): SetFont/Pos: %s", time.Now().Sub(fontSt).String())
	}
	// Push to current page
	drawSt := time.Now()
	err = c.Draw(p)
	if t.Benchmark {
		log.Printf("-- RenderElement(): Draw: %s", time.Now().Sub(drawSt).String())
	}
	return err
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
