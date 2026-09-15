package jobqueue

import (
	"strings"
	"testing"

	"github.com/freemed/remitt-server/config"
)

// TestRenderPropagatesTransformError pins the bug fixed in (*JobQueueItem).Render:
// the transform error used to be assigned and then immediately overwritten by
// the following os.ReadFile, so a render whose transform failed returned an empty
// payload with a NIL error. A job would then be treated as successful and
// transported with no document at all.
func TestRenderPropagatesTransformError(t *testing.T) {
	prev := config.Config
	t.Cleanup(func() { config.Config = prev })

	cfg := &config.AppConfig{}
	cfg.SetDefaults()
	cfg.Paths.BasePath = t.TempDir() // no resources/xsl beneath it
	cfg.Paths.XsltProcPath = ""      // and nothing to fall back to: the error must surface
	cfg.InternalXslt = true
	config.Config = cfg

	item := &JobQueueItem{
		Payload:      []byte(`<remitt><global><billinguid>B1</billinguid></global></remitt>`),
		RenderOption: "statement",
	}

	out, err := item.Render()
	if err == nil {
		t.Fatalf("Render() returned a nil error for a missing stylesheet (returned %d bytes): "+
			"the transform error is being discarded again", len(out))
	}
	if !strings.Contains(err.Error(), "statement.xsl") {
		t.Errorf("error does not name the stylesheet it could not use: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("a failed render must not return output, got %d bytes", len(out))
	}
}

// TestRenderUsesTheConfiguredEngine pins that the job pipeline honors
// internal-xslt instead of hardcoding the external binary. The internal branch
// used to be commented out here, so flipping the config default would have had
// no effect on the path that actually renders production jobs.
func TestRenderUsesTheConfiguredEngine(t *testing.T) {
	prev := config.Config
	t.Cleanup(func() { config.Config = prev })

	// No xsltproc path is configured. If Render still called the external engine
	// directly, this must fail; with the in-process engine selected, a missing
	// stylesheet is the only error, and its message must not come from exec.
	cfg := &config.AppConfig{}
	cfg.SetDefaults()
	cfg.Paths.BasePath = t.TempDir()
	cfg.Paths.XsltProcPath = ""
	cfg.InternalXslt = true
	config.Config = cfg

	item := &JobQueueItem{Payload: []byte(""), RenderOption: "4010_837p"}
	if _, err := item.Render(); err == nil {
		t.Fatal("expected an error: the stylesheet does not exist under the temporary base path")
	} else if strings.Contains(err.Error(), "no command") || strings.Contains(err.Error(), "exec") {
		t.Errorf("Render() invoked the external binary although no path is configured and the "+
			"in-process engine is selected: %v", err)
	}
}
