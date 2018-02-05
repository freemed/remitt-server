package translation

import (
	"bytes"
	//"fmt"
	"os"

	"github.com/orcaman/writerseeker"
	"github.com/freemed/remitt-server/model"
	"github.com/unidoc/unidoc/pdf/creator"
	pdf "github.com/unidoc/unidoc/pdf/model"
	"github.com/unidoc/unidoc/pdf/model/fonts"
)

type TranslateFixedFormPDF struct {
}

func (self *TranslateFixedFormPDF) Translate(source model.FixedFormXml) (out []byte, err error) {
	// Create new PDF factory with unidoc
	c := creator.New()
	for iter, _ := range source.Pages {
		err = self.RenderPage(c, source.Pages[iter])
		if err != nil {
			return 
		}
	}
	writerSeeker := &writerseeker.WriterSeeker{}
	err = c.Write(writerSeeker)
	if err != nil {
		reader := writerSeeker.Reader()
		buf := new(bytes.Buffer)
		buf.ReadFrom(reader)
		out = buf.Bytes()
	}
	return
}

func (self *TranslateFixedFormPDF) RenderPage(c *creator.Creator, pageObj model.FixedFormPage) (err error) {
	if pageObj.Format.Pdf.Template != "" {
		f, err := os.Open(pageObj.Format.Pdf.Template)
		if err != nil {
			return err
		}
		defer f.Close()

		pdfReader, err := pdf.NewPdfReader(f)
		if err != nil {
			return err
		}

		templatePage, err := pdfReader.GetPage(pageObj.Format.Pdf.Page + 1)
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
	content := element.Content
	if element.OmitPdf {
		return nil
	}
	if element.PeriodStripPdf {
		// TODO: FIXME: IMPLEMENT
	}

	// TODO: FIXME: pad or cut if necessary

	p := creator.NewParagraph(content)

	// Column / X
	xPos := float64((float64(element.Column) * pageObj.Format.Pdf.Scaling.Horizontal) + pageObj.Format.Pdf.Offset.Horizontal)
	// Row / Y
	yPos := float64((float64(element.Row) * pageObj.Format.Pdf.Scaling.Vertical) + pageObj.Format.Pdf.Offset.Vertical)

	p.SetFont(fonts.NewFontCourier())
	p.SetFontSize(pageObj.Format.Pdf.Font.Size)
	p.SetPos(xPos, yPos)

	// Push to current page
	err = c.Draw(p)
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
