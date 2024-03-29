package translation

import (
	"context"
	"errors"
	"fmt"

	"github.com/freemed/remitt-server/model"
)

func init() {
	RegisterTranslator("x12xml", func() Translator { return &TranslateX12Xml{} })
}

type TranslateX12Xml struct {
	Hl        map[string]int
	HlCounter int
	Counters  map[string]int
	ctx       context.Context
}

func (self *TranslateX12Xml) Resolver(in string, out string) bool {
	return (in == "x12xml" && out == "x12") || (in == "x12xml" && out == "*")
}

func (self *TranslateX12Xml) Translate(source any) (out []byte, err error) {
	src, ok := source.(model.X12Xml)
	if !ok {
		out = []byte{}
		err = errors.New("invalid datatype presented")
		return
	}

	self.Hl = map[string]int{}
	self.Counters = map[string]int{}
	self.HlCounter = 0
	outString := ""
	for iter := range src.Segments {
		r, err := self.RenderSegment(src, src.Segments[iter], iter)
		if err != nil {
			return out, err
		}
		outString += r
	}
	out = []byte(outString)
	return
}

func (self *TranslateX12Xml) SetContext(ctx context.Context) error {
	self.ctx = ctx
	return nil
}

func (self *TranslateX12Xml) RightPad(text string, length int) string {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x += " "
	}
	return x
}

func (self *TranslateX12Xml) LeftZeroPad(text string, length int) string {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x = "0" + x
	}
	return x
}

func (self *TranslateX12Xml) RenderSegment(source model.X12Xml, segment model.X12Segment, segmentCount int) (out string, err error) {
	l := make([]string, 0)
	for _, el := range segment.Elements {
		content := ""

		if el.SegmentCount != "" {
			l = append(l, fmt.Sprintf("%d", segmentCount-2))
			continue
		}

		// Reset counter
		if el.ResetCounter.Name != "" {
			self.Counters[el.ResetCounter.Name] = 0
		}

		if el.Counter.Name != "" {
			_, ok := self.Counters[el.Counter.Name]
			if !ok {
				self.Counters[el.Counter.Name] = 1
			} else {
				self.Counters[el.Counter.Name]++
			}
			l = append(l, fmt.Sprintf("%d", self.Counters[el.Counter.Name]))
			continue
		}

		if el.Hl != "" {
			_, ok := self.Hl[el.Hl]
			if !ok {
				self.HlCounter++
				self.Hl[el.Hl] = self.HlCounter
				content = fmt.Sprintf("%d", self.Hl[el.Hl])
			} else {
				content = fmt.Sprintf("%d", self.Hl[el.Hl])
			}
		} else {
			content = el.Content.Content
			if content == "" {
				content = el.Content.Text
			}
			if el.Content.FixedLength != 0 {
				if len(content) > el.Content.FixedLength {
					content = content[0:el.Content.FixedLength]
				} else if len(content) < el.Content.FixedLength {
					content = self.RightPad(content, el.Content.FixedLength)
				}
			}
			if el.Content.ZeroPrepend != 0 {
				if len(content) > el.Content.ZeroPrepend {
					content = content[0:el.Content.ZeroPrepend]
				} else if len(content) < el.Content.ZeroPrepend {
					content = self.LeftZeroPad(content, el.Content.ZeroPrepend)
				}
			}
		}

		l = append(l, content)
	}

	out = ""
	out = out + segment.SegmentId
	out = out + source.X12Format.Delimiter

	elementCount := len(l)
	for iter := 0; iter < elementCount; iter++ {
		out += l[iter]
		if iter < (elementCount - 1) {
			out += source.X12Format.Delimiter
		}
	}
	out += source.X12Format.EndOfLine

	return
}
