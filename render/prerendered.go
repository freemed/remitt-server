package render

import "context"

func init() {
	RegisterRenderer("org.remitt.plugin.render.PreRenderedPlugin", func() Renderer { return &PreRenderedPlugin{} })
}

// PreRenderedPlugin passes through pre-rendered content unchanged.
type PreRenderedPlugin struct {
	ctx context.Context
}

func (p *PreRenderedPlugin) Render(input []byte, option string) ([]byte, error) {
	// Pass through pre-rendered content unchanged
	return input, nil
}

func (p *PreRenderedPlugin) SetContext(ctx context.Context) error {
	p.ctx = ctx
	return nil
}
