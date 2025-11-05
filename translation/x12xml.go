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

func (t *TranslateX12Xml) Resolver(in string, out string) bool {
	return (in == "x12xml" && out == "x12") || (in == "x12xml" && out == "*")
}

func (t *TranslateX12Xml) Translate(source any) (out []byte, err error) {
	src, ok := source.(model.X12Xml)
	if !ok {
		out = []byte{}
		err = errors.New("x12xml: translate: invalid datatype presented")
		return
	}

	t.Hl = map[string]int{}
	t.Counters = map[string]int{}
	t.HlCounter = 0
	outString := ""
	for iter := range src.Segments {
		r, err := t.RenderSegment(src, src.Segments[iter], iter)
		if err != nil {
			return out, fmt.Errorf("x12xml: translate: rendersegment: %w", err)
		}
		outString += r
	}
	out = []byte(outString)
	return
}

func (t *TranslateX12Xml) SetContext(ctx context.Context) error {
	t.ctx = ctx
	return nil
}

func (t *TranslateX12Xml) RenderSegment(source model.X12Xml, segment model.X12Segment, segmentCount int) (out string, err error) {
	l := make([]string, 0)
	for _, el := range segment.Elements {
		content := ""

		if el.SegmentCount != "" {
			l = append(l, fmt.Sprintf("%d", segmentCount-2))
			continue
		}

		// Reset counter
		if el.ResetCounter.Name != "" {
			t.Counters[el.ResetCounter.Name] = 0
		}

		if el.Counter.Name != "" {
			_, ok := t.Counters[el.Counter.Name]
			if !ok {
				t.Counters[el.Counter.Name] = 1
			} else {
				t.Counters[el.Counter.Name]++
			}
			l = append(l, fmt.Sprintf("%d", t.Counters[el.Counter.Name]))
			continue
		}

		if el.Hl != "" {
			_, ok := t.Hl[el.Hl]
			if !ok {
				t.HlCounter++
				t.Hl[el.Hl] = t.HlCounter
				content = fmt.Sprintf("%d", t.Hl[el.Hl])
			} else {
				content = fmt.Sprintf("%d", t.Hl[el.Hl])
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
					content = t.rightPad(content, el.Content.FixedLength)
				}
			}
			if el.Content.ZeroPrepend != 0 {
				if len(content) > el.Content.ZeroPrepend {
					content = content[0:el.Content.ZeroPrepend]
				} else if len(content) < el.Content.ZeroPrepend {
					content = t.leftZeroPad(content, el.Content.ZeroPrepend)
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

func (t *TranslateX12Xml) rightPad(text string, length int) string {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x += " "
	}
	return x
}

func (t *TranslateX12Xml) leftZeroPad(text string, length int) string {
	x := text
	for iter := 0; iter < length-len(text); iter++ {
		x = "0" + x
	}
	return x
}

