package common

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestXslTransform_Compare runs each XSL transform through both ratago
// (internal) and xsltproc (external), then diffs the output.
//
// Updated with ratago that implements EXSLT modules 1-8 (set:distinct,
// func:function, date functions, etc.) and a pure-Go gokogiri (no CGo).
func TestXslTransform_Compare(t *testing.T) {
	if _, err := exec.LookPath("xsltproc"); err != nil {
		t.Skip("xsltproc not available on this system")
	}

	dir := resolveXslDir(t)

	input := `<remitt>
	<global><currentdate><year>2024</year><month>08</month><day>10</day></currentdate>
	<currenttime><hour>12</hour><minute>00</minute></currenttime>
	<billinguid>BILL-001</billinguid></global>
	<clearinghouse><name>TEST CH</name><etin>123456789</etin>
	<x12gssenderid>SID</x12gssenderid><x12gsreceiverid>RID</x12gsreceiverid></clearinghouse>
	<billingservice><name>BS</name><etin>987654321</etin></billingservice>
	<billingcontact><name>CONTACT</name><phone><area>555</area><number>1234567</number>
	<extension>101</extension></phone></billingcontact>
	<practice id="1"><name>PRACTICE</name><npi>1234567890</npi><address1>123 MAIN</address1>
	<city>ANYTOWN</city><state>NY</state><zip>10001</zip>
	<phone><area>555</area><number>7654321</number></phone><tin>123456789</tin></practice>
	<provider id="1"><name>PROVIDER</name><npi>0987654321</npi></provider>
	<facility id="1"><name>FACILITY</name><address1>456 OAK</address1><city>ANYTOWN</city>
	<state>NY</state><zip>10001</zip><npi>1111111111</npi></facility>
	<payer id="1"><name>PAYER</name><payerid>P01</payerid></payer>
	<patient id="1"><lastname>DOE</lastname><firstname>JOHN</firstname><mi>Q</mi>
	<dob><year>1980</year><month>01</month><day>15</day></dob><sex>M</sex>
	<address1>789 ELM</address1><city>ANYTOWN</city><state>NY</state><zip>10001</zip>
	<phone><area>555</area><number>1112222</number></phone></patient>
	<insured id="1"><lastname>DOE</lastname><firstname>JOHN</firstname><mi>Q</mi>
	<relationship>18</relationship><groupid>GRP001</groupid><memberid>MEM001</memberid></insured>
	<diagnosis id="1"><code>E11.9</code></diagnosis>
	<procedure id="1"><practicekey>1</practicekey><patientkey>1</patientkey><insuredkey>1</insuredkey>
	<providerkey>1</providerkey><payerkey>1</payerkey><facilitykey>1</facilitykey>
	<diagnosiskey>1</diagnosiskey><charge>150.00</charge><units>1</units><cpt>99213</cpt>
	<mod1></mod1><mod2></mod2><mod3></mod3><mod4></mod4>
	<dosfrom><year>2024</year><month>08</month><day>01</day></dosfrom>
	<dosto><year>2024</year><month>08</month><day>01</day></dosto></procedure>
</remitt>`

	params := map[string]string{
		"jobId":       "1",
		"currentTime": "20240810120000",
	}

	xslFiles := []string{
		"4010_837p.xsl",
		"5010_837p.xsl",
		"cms1500.xsl",
		"statement.xsl",
	}

	results := make(map[string]string) // name -> "MATCH" or "DIFFER" or "RATAGO_FAIL" etc.

	for _, xslName := range xslFiles {
		name := strings.TrimSuffix(xslName, ".xsl")
		t.Run(name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "xslt-cmp-*")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)

			xslPath := filepath.Join(dir, xslName)

			// --- ratago ---
			inR := filepath.Join(tmpDir, "ratago_in.xml")
			outR := filepath.Join(tmpDir, "ratago_out.xml")
			if err := os.WriteFile(inR, []byte(input), 0o644); err != nil {
				t.Fatal(err)
			}
			errR := XslTransformInternal(inR, xslPath, outR, params)
			var rStr string
			if errR != nil {
				results[name] = "RATAGO_FAIL: " + errR.Error()
			} else {
				rOut, _ := os.ReadFile(outR)
				rStr = strings.TrimSpace(string(rOut))
			}

			// --- xsltproc ---
			inX := filepath.Join(tmpDir, "xsltproc_in.xml")
			outX := filepath.Join(tmpDir, "xsltproc_out.xml")
			if err := os.WriteFile(inX, []byte(input), 0o644); err != nil {
				t.Fatal(err)
			}
			args := []string{
				"--stringparam", "jobId", params["jobId"],
				"--stringparam", "currentTime", params["currentTime"],
				"-o", outX, xslPath, inX,
			}
			cmd := exec.Command("xsltproc", args...)
			xProcOut, errX := cmd.CombinedOutput()
			var xStr string
			if errX != nil {
				results[name] = "XSLTPROC_FAIL: " + errX.Error()
			} else {
				xOut, _ := os.ReadFile(outX)
				xStr = strings.TrimSpace(string(xOut))
			}

			// --- compare ---
			switch {
			case errR != nil && errX != nil:
				t.Errorf("BOTH engines failed\n  ratago:  %v\n  xsltproc: %v / %s", errR, errX, string(xProcOut))
				results[name] = "BOTH_FAIL"
			case errR != nil:
				t.Errorf("ratago FAILED, xsltproc OK\n  ratago: %v", errR)
				results[name] = "RATAGO_FAIL"
			case errX != nil:
				t.Errorf("ratago OK, xsltproc FAILED: %v / %s", errX, string(xProcOut))
				results[name] = "XSLTPROC_FAIL"
			case rStr == xStr:
				t.Logf("MATCH — identical output (%d bytes)", len(rStr))
				results[name] = "MATCH"
			default:
				t.Errorf("DIFFER — ratago: %d bytes, xsltproc: %d bytes", len(rStr), len(xStr))
				results[name] = "DIFFER"
			}
		})
	}

	t.Log("--- summary ---")
	for _, name := range xslFiles {
		n := strings.TrimSuffix(name, ".xsl")
		t.Logf("  %s: %s", n, results[n])
	}
}

func resolveXslDir(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(d) == "common" {
		d = filepath.Dir(d)
	}
	return filepath.Join(d, "resources", "xsl")
}
