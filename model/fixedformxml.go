package model

import (
	"encoding/xml"
)

type FixedFormXml struct {
	XMLName xml.Name        `xml:"fixedform"`
	Pages   []FixedFormPage `xml:"page"`
}

type FixedFormPage struct {
	Format struct {
		PageLength int `xml:"pagelength"`
		Pdf        struct {
			Template string `xml:"template,attr"`
			Page     int    `xml:"page,attr"`
			Font     struct {
				Name string `xml:"name,attr"`
				Size float64  `xml:"size,attr"`
			} `xml:"font"`
			Scaling struct {
				Vertical   float64 `xml:"vertical,attr"`
				Horizontal float64 `xml:"horizontal,attr"`
			} `xml:"scaling"`
			Offset struct {
				Vertical   float64 `xml:"vertical,attr"`
				Horizontal float64 `xml:"horizontal,attr"`
			} `xml:"offset"`
		} `xml:"pdf"`
	} `xml:"format"`
	Elements []FixedElement `xml:"element"`
}

type FixedElement struct {
	OmitPdf        bool   `xml:"omitpdf,attr"`
	PeriodStripPdf bool   `xml:"periodstrippdf,attr"`
	Row            int    `xml:"row"`
	Column         int    `xml:"column"`
	Length         int    `xml:"length"`
	Content        string `xml:"content"`
}
