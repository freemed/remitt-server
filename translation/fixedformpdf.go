package translation

import (
	"bytes"
	"errors"

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

func (self *TranslateFixedFormPDF) Resolver(in string, out string) bool {
	return (in == "fixedformxml" && out == "pdf")
}

func (self *TranslateFixedFormPDF) Translate(source interface{}) (out []byte, err error) {
	st := time.Now()

	if self.Debug {
		log.Printf("Translate()")
	}

	src, ok := source.(model.FixedFormXml)
	if !ok {
		err = errors.New("invalid datatype presented")
	}

	if self.Benchmark {
		log.Printf("Conversion : %s", time.Now().Sub(st).String())
	}

	// Create new PDF factory with unidoc
	c := creator.New()
	for iter, _ := range src.Pages {
		err = self.RenderPage(c, src.Pages[iter])
		if err != nil {
			return
		}
		if self.Benchmark {
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

func (self *TranslateFixedFormPDF) RenderPage(c *creator.Creator, pageObj model.FixedFormPage) (err error) {
	if self.Debug {
		log.Printf("RenderPage()")
	}
	if pageObj.Format.Pdf.Template != "" {
		f, err := os.Open(self.TemplatePath + string(os.PathSeparator) + pageObj.Format.Pdf.Template + ".pdf")
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

	for iter, _ := range pageObj.Elements {
		err = self.RenderElement(c, pageObj, pageObj.Elements[iter])
		if err != nil {
			return err
		}
	}

	return nil
}

func (self *TranslateFixedFormPDF) RenderElement(c *creator.Creator, pageObj model.FixedFormPage, element model.FixedElement) (err error) {
	st := time.Now()

	if self.Debug {
		log.Printf("RenderElement(%#v)", element)
	}

	content := element.Content
	if element.OmitPdf {
		return nil
	}
	if element.PeriodStripPdf {
		// TODO: FIXME: IMPLEMENT
	}

	// TODO: FIXME: pad or cut if necessary

	p := creator.NewParagraph(content)
	if self.Benchmark {
		log.Printf("-- RenderElement(): NewParagraph: %s", time.Now().Sub(st).String())
	}

	// Column / X
	xPos := float64((float64(element.Column) * pageObj.Format.Pdf.Scaling.Horizontal) + pageObj.Format.Pdf.Offset.Horizontal)
	// Row / Y
	yPos := float64((float64(element.Row) * pageObj.Format.Pdf.Scaling.Vertical) + pageObj.Format.Pdf.Offset.Vertical)

	font_st := time.Now()

	p.SetFont(fonts.NewFontCourier())
	p.SetFontSize(pageObj.Format.Pdf.Font.Size)
	p.SetPos(xPos, yPos)

	if self.Benchmark {
		log.Printf("-- RenderElement(): SetFont/Pos: %s", time.Now().Sub(font_st).String())
	}
	// Push to current page
	draw_st := time.Now()
	err = c.Draw(p)
	if self.Benchmark {
		log.Printf("-- RenderElement(): Draw: %s", time.Now().Sub(draw_st).String())
	}
	return err
}

func (self *TranslateFixedFormPDF) RightPad(text string, length int) string {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x += " "
	}
	return x
}

func (self *TranslateFixedFormPDF) LeftZeroPad(text string, length int) string {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x = "0" + x
	}
	return x
}
