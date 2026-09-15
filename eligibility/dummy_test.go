package eligibility

import "testing"

// TestDummyCheckerResolvesUnderBothNames pins a database-alignment defect found by
// auditing every registry against the names the database actually stores: this
// checker was registered ONLY as "DummyEligibility" while tPlugins holds the Java
// FQCN (migrations/001_legacy.up.sql:229,
// 'org.remitt.plugin.eligibility.DummyEligibility'), so the seeded value failed
// with "unable to locate eligibility checker
// org.remitt.plugin.eligibility.DummyEligibility" for task/eligibility.go and
// api/eligibility.go. The short name must keep working too - the SOAP layer
// submits it (soap/soap_test.go).
//
// This is the same defect class that left the translation registry unable to
// answer to the values in tTranslation, and the same fix shape transport already
// used for its Java FQCNs.
func TestDummyCheckerResolvesUnderBothNames(t *testing.T) {
	for _, name := range []string{
		"DummyEligibility",
		"org.remitt.plugin.eligibility.DummyEligibility",
	} {
		t.Run(name, func(t *testing.T) {
			checker, err := InstantiateChecker(name)
			if err != nil {
				t.Fatalf("InstantiateChecker(%q) failed: %v - the database stores this name "+
					"(tPlugins) and the seeded value must resolve", name, err)
			}
			if checker == nil {
				t.Fatalf("InstantiateChecker(%q) returned a nil checker with no error", name)
			}
			if _, ok := checker.(*DummyEligibility); !ok {
				t.Errorf("InstantiateChecker(%q) returned %T; want *DummyEligibility", name, checker)
			}
		})
	}
}
