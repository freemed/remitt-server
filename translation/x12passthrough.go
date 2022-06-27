package translation

import (
	"context"
	"fmt"
)

func init() {
	RegisterTranslator("x12passthrough", func() Translator { return &TranslateX12Passthrough{} })
}

type TranslateX12Passthrough struct {
	ctx context.Context
}

func (self *TranslateX12Passthrough) Resolver(in string, out string) bool {
	return (in == "x12" && out == "x12") || (in == "x12" && out == "*")
}

func (self *TranslateX12Passthrough) Translate(source any) (out []byte, err error) {
	src, works := source.([]byte)
	if !works {
		return []byte{}, fmt.Errorf("x12 bytes not provided")
	}
	return src, nil
}

func (self *TranslateX12Passthrough) SetContext(ctx context.Context) error {
	self.ctx = ctx
	return nil
}
