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
				Name string  `xml:"name,attr"`
				Size float64 `xml:"size,attr"`
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
	Elements FixedElements `xml:"element"`
}

type FixedElements []FixedElement

func (s FixedElements) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s FixedElements) Less(i, j int) bool {
	if s[i].Row != s[j].Row {
		return s[i].Row < s[j].Row
	}
	if s[i].Column != s[j].Column {
		return s[i].Column < s[j].Column
	}
	return false
}

func (s FixedElements) Len() int {
	return len(s)
}

type FixedElement struct {
	OmitPdf        bool   `xml:"omitpdf,attr"`
	PeriodStripPdf bool   `xml:"periodstrippdf,attr"`
	Row            int    `xml:"row"`
	Column         int    `xml:"column"`
	Length         int    `xml:"length"`
	Content        string `xml:"content"`
}
