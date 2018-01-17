package model

import (
	"encoding/xml"
)

type X12Xml struct {
	XMLName   xml.Name `xml:"render"`
	X12Format struct {
		Delimiter string `xml:"delimiter"`
		EndOfLine string `xml:"endofline"`
	} `xml:"x12format"`
	Segments []X12Segment `xml:"x12segment"`
}

type X12Segment struct {
	SegmentId string       `xml:"sid,attr"`
	Comment   string       `xml:"comment"`
	Elements  []X12Element `xml:"element"`
}

type X12Element struct {
	Comment string `xml:"comment,omitempty"`
	Hl      string `xml:"hl"`
	Counter struct {
		Counter string `xml:"counter"`
		Name    string `xml:"name,attr"`
	} `xml:"counter"`
	SegmentCount string `xml:"segmentcount"`
	Content      struct {
		Content     string `xml:",innerxml"`
		Text        string `xml:"text,attr"`
		FixedLength int    `xml:"fixedlength,attr"`
		ZeroPrepend int    `xml:"zeroprepend,attr"`
	} `xml:"content"`
}
