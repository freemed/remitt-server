package common

import (
	"os"
	"path/filepath"
	"testing"
)

// TestXslTransform_MatchVariants tests different match patterns to isolate
// the root-element template matching bug in pure-Go ratago.
func TestXslTransform_MatchVariants(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xslt-match-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	input := `<remitt><child>data</child></remitt>`

	tests := []struct {
		name, match, xsl, expect string
	}{
		{
			name:  "match_remitt_no_slash",
			match: `match="remitt"`,
			xsl: `<?xml version="1.0"?>
<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="xml" indent="yes"/>
  <xsl:template match="remitt">
    <render><test>MATCHED</test></render>
  </xsl:template>
</xsl:stylesheet>`,
			expect: "MATCHED",
		},
		{
			name:  "match_slash_remitt",
			match: `match="/remitt"`,
			xsl: `<?xml version="1.0"?>
<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="xml" indent="yes"/>
  <xsl:template match="/remitt">
    <render><test>MATCHED</test></render>
  </xsl:template>
</xsl:stylesheet>`,
			expect: "MATCHED",
		},
		{
			name:  "match_slash",
			match: `match="/"`,
			xsl: `<?xml version="1.0"?>
<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="xml" indent="yes"/>
  <xsl:template match="/">
    <render><test>ROOT_MATCHED</test></render>
  </xsl:template>
</xsl:stylesheet>`,
			expect: "ROOT_MATCHED",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			xslPath := filepath.Join(tmpDir, tc.name+".xsl")
			inPath := filepath.Join(tmpDir, tc.name+"_in.xml")
			outPath := filepath.Join(tmpDir, tc.name+"_out.xml")

			os.WriteFile(xslPath, []byte(tc.xsl), 0o644)
			os.WriteFile(inPath, []byte(input), 0o644)

			err := XslTransformInternal(inPath, xslPath, outPath, nil)
			if err != nil {
				t.Fatalf("ratago error: %v", err)
			}
			out, _ := os.ReadFile(outPath)
			outStr := string(out)
			if contains(outStr, tc.expect) {
				t.Logf("MATCHED — found %q in output (%d bytes)", tc.expect, len(out))
			} else {
				t.Errorf("NOT MATCHED — expected %q, got (%d bytes):\n%s", tc.expect, len(out), outStr)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
