package common

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/config"
)

// xslDispatchTestInput mirrors the payload used by TestXslTransform_Compare so
// that the shipped stylesheets have every element they read.
const xslDispatchTestInput = `<remitt>
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

// withTestConfig swaps the process-wide config for the duration of a test.
func withTestConfig(t *testing.T, cfg *config.AppConfig) {
	t.Helper()
	prev := config.Config
	config.Config = cfg
	t.Cleanup(func() { config.Config = prev })
}

// TestXslTransformDispatchesOnConfig pins the engine-selection contract that the
// `internal-xslt` default rests on: with the flag ON the transform goes through
// the in-process engine, with it OFF through the xsltproc binary, and for a
// shipped stylesheet the two produce IDENTICAL bytes. If that last assertion
// ever fails, the in-process engine is no longer a safe default.
func TestXslTransformDispatchesOnConfig(t *testing.T) {
	xsltproc, err := exec.LookPath("xsltproc")
	if err != nil {
		t.Skip("xsltproc not available on this system")
	}

	dir := resolveXslDir(t)
	xslFile := filepath.Join(dir, "statement.xsl")
	if _, err := os.Stat(xslFile); err != nil {
		t.Fatalf("stylesheet not found: %v", err)
	}

	inFile := filepath.Join(t.TempDir(), "dispatch-in.xml")
	if err := os.WriteFile(inFile, []byte(xslDispatchTestInput), 0o644); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"jobId": "1", "currentTime": "20240810120000"}

	// In-process engine (the default).
	cfg := &config.AppConfig{}
	cfg.SetDefaults()
	if !cfg.InternalXslt {
		t.Fatal("SetDefaults() no longer selects the in-process engine: the " +
			"internal-xslt default was changed, so this test no longer covers the default path")
	}
	outInternal := filepath.Join(t.TempDir(), "internal.out")
	withTestConfig(t, cfg)
	if err := XslTransform(inFile, xslFile, outInternal, params); err != nil {
		t.Fatalf("in-process engine failed: %v", err)
	}

	// External engine.
	cfgExternal := &config.AppConfig{}
	cfgExternal.SetDefaults()
	cfgExternal.InternalXslt = false
	cfgExternal.Paths.XsltProcPath = xsltproc
	outExternal := filepath.Join(t.TempDir(), "external.out")
	withTestConfig(t, cfgExternal)
	if err := XslTransform(inFile, xslFile, outExternal, params); err != nil {
		t.Fatalf("external engine failed: %v", err)
	}

	internalBytes, err := os.ReadFile(outInternal)
	if err != nil {
		t.Fatal(err)
	}
	externalBytes, err := os.ReadFile(outExternal)
	if err != nil {
		t.Fatal(err)
	}

	if string(internalBytes) != string(externalBytes) {
		t.Errorf("engine selection changes the output: in-process %d bytes, xsltproc %d bytes%s",
			len(internalBytes), len(externalBytes),
			firstDivergence(normalizeForCompare(string(internalBytes)), normalizeForCompare(string(externalBytes))))
	}
}

// TestXslTransformFallbackIsConditional pins that the xsltproc fallback happens
// only when a binary is actually configured. With no configured binary the real
// cause must be reported instead of a misleading "exec: no command" from an
// attempted fallback.
func TestXslTransformFallbackIsConditional(t *testing.T) {
	// A stylesheet that does not exist fails in the in-process engine.
	xslFile := filepath.Join(t.TempDir(), "does-not-exist.xsl")
	inFile := filepath.Join(t.TempDir(), "in.xml")
	if err := os.WriteFile(inFile, []byte(xslDispatchTestInput), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.AppConfig{}
	cfg.SetDefaults()
	cfg.Paths.XsltProcPath = "" // nothing to fall back to
	withTestConfig(t, cfg)

	err := XslTransform(inFile, xslFile, filepath.Join(t.TempDir(), "out.xml"), nil)
	if err == nil {
		t.Fatal("expected the missing stylesheet to be reported as an error")
	}
	if strings.Contains(err.Error(), "fallback") {
		t.Errorf("no fallback binary was configured, so none should have been attempted: %v", err)
	}
	if !strings.Contains(err.Error(), "does-not-exist.xsl") {
		t.Errorf("error does not name the missing stylesheet: %v", err)
	}

	// Now configure a binary that cannot run: the error must name BOTH engines so
	// the operator can see the in-process failure was not the whole story.
	cfgFallback := &config.AppConfig{}
	cfgFallback.SetDefaults()
	cfgFallback.Paths.XsltProcPath = filepath.Join(t.TempDir(), "no-such-xsltproc")
	withTestConfig(t, cfgFallback)

	err = XslTransform(inFile, xslFile, filepath.Join(t.TempDir(), "out2.xml"), nil)
	if err == nil {
		t.Fatal("expected an error when both engines fail")
	}
	if !strings.Contains(err.Error(), "in-process engine failed") ||
		!strings.Contains(err.Error(), "fallback failed") {
		t.Errorf("error should name both failures, got: %v", err)
	}
}
