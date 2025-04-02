package translation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"sort"

	"github.com/freemed/remitt-server/model"
)

func init() {
	RegisterTranslator("fixedformxml", func() Translator { return &TranslateFixedFormXML{} })
}

type TranslateFixedFormXML struct {
	Debug bool
	ctx   context.Context
}

func (t *TranslateFixedFormXML) Resolver(in string, out string) bool {
	return (in == "fixedformxml" && out == "text") || (in == "fixedformxml" && out == "*")
}

func (t *TranslateFixedFormXML) Translate(source any) (out []byte, err error) {
	if t.Debug {
		log.Printf("Translate()")
	}

	src, ok := source.(model.FixedFormXml)
	if !ok {
		err = errors.New("invalid datatype presented")
		return
	}

	ob := &bytes.Buffer{}

	for iter := range src.Pages {
		if t.Debug {
			log.Printf("TranslateFixedFormXML: Processing page %d", iter)
		}

		// Sort elements
		sort.Sort(src.Pages[iter].Elements)

		err = t.RenderPage(ob, src.Pages[iter])
		if err != nil {
			return ob.Bytes(), err
		}
	}

	return ob.Bytes(), nil
}

func (t *TranslateFixedFormXML) SetContext(ctx context.Context) error {
	t.ctx = ctx
	return nil
}

func (t *TranslateFixedFormXML) RenderPage(o io.Writer, pageObj model.FixedFormPage) (err error) {
	if t.Debug {
		log.Printf("RenderPage()")
	}

	row := 1
	col := 1

	for iter := range pageObj.Elements {
		// Pad to appropriate position
		spacing := t.padToPosition(row, col, pageObj.Elements[iter].Row, pageObj.Elements[iter].Column)
		_, err := o.Write(spacing)
		if err != nil {
			log.Printf("TranslateFixedFormPDF.RenderPage(): Write(): ERR: %s", err.Error())
			return err
		}

		// Store new position
		row = pageObj.Elements[iter].Row
		col = pageObj.Elements[iter].Column + pageObj.Elements[iter].Length

		el, err := t.renderElement(pageObj, pageObj.Elements[iter])
		if err != nil {
			log.Printf("TranslateFixedFormPDF.RenderPage(): RenderElement(): ERR: %s", err.Error())
			return err
		}
		_, err = o.Write(el)
		if err != nil {
			log.Printf("TranslateFixedFormPDF.RenderPage(): Write(): ERR: %s", err.Error())
			return err
		}
	}

	return nil
}

func (t *TranslateFixedFormXML) renderElement(pageObj model.FixedFormPage, element model.FixedElement) ([]byte, error) {
	if t.Debug {
		log.Printf("RenderElement(%#v)", element)
	}

	content := []byte(element.Content)

	// Cut if necessary to avoid overruns
	if len(content) > element.Length {
		content = content[0:element.Length]
	}

	return t.rightPad(content, element.Length), nil
}

func (t *TranslateFixedFormXML) rightPad(text []byte, length int) []byte {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x = append(x, ' ')
	}
	return x
}

func (t *TranslateFixedFormXML) padToPosition(oldRow, oldColumn, newRow, newColumn int) []byte {
	// Sanity checks
	if oldRow > newRow {
		//log.debug("oldRow = " + oldRow + ", newRow = " + newRow);
		return []byte{}
	}
	if (oldRow == newRow) && (oldColumn > newColumn) {
		//log.debug("oldRow = " + oldRow + ", newRow = " + newRow + " ( " + oldColumn + " > " + newColumn + " )");
		return []byte{}
	}
	if (oldRow == newRow) && (oldColumn == newColumn) {
		return []byte{}
	}

	buf := []byte{}
	currentRow := oldRow
	currentColumn := oldColumn

	for currentRow < newRow {
		buf = append(buf, '\r', '\n')
		currentRow += 1
		currentColumn = 1
	}

	for currentColumn < newColumn {
		buf = append(buf, ' ')
		currentColumn += 1
	}

	return buf
}
