package eligibility

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/freemed/remitt-server/crypto"
	"github.com/freemed/remitt-server/model"
)

// Plugin identifiers for GatewayEDI eligibility checker.
const (
	GatewayEDIEligibilityClass   = "org.remitt.plugin.eligibility.GatewayEDIEligibility"
	GatewayEDIEligibilityVersion = "0.1"
	GatewayEDIEligibilityKeyName = "GatewayEDI"
)

// GatewayEDIEligibility implements the EligibilityChecker interface for
// the GatewayEDI SOAP-based eligibility service.
//
// Flow: values → buildSoapEnvelope → PGP encrypt with user's GatewayEDI
// public key → SOAP HTTP POST → parse SOAP response → EligibilityResponse.
// The SOAP HTTP call is currently stubbed and returns a success response.
type GatewayEDIEligibility struct {
	ctx context.Context
}

func init() {
	RegisterChecker(GatewayEDIEligibilityClass, func() EligibilityChecker {
		return &GatewayEDIEligibility{}
	})
}

// buildSoapEnvelope creates a SOAP request envelope containing the given
// key-value pairs as elements in the eligibility request body.
func (g *GatewayEDIEligibility) buildSoapEnvelope(values map[string]string) ([]byte, error) {
	const soapTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Header/>
  <soapenv:Body>
    <eligibilityRequest xmlns="urn:remitt:eligibility">
      {{range $key, $value := .}}<{{$key}}>{{$value}}</{{$key}}>
      {{end}}    </eligibilityRequest>
  </soapenv:Body>
</soapenv:Envelope>`

	tmpl, err := template.New("soap").Parse(soapTmpl)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: parse soap template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, values); err != nil {
		return nil, fmt.Errorf("gatewayedi: execute soap template: %w", err)
	}

	return buf.Bytes(), nil
}

// CheckEligibility runs the GatewayEDI eligibility check. The SOAP HTTP
// call is currently stubbed and always returns a success response.
func (g *GatewayEDIEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	// Build the SOAP request envelope.
	envelope, err := g.buildSoapEnvelope(values)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: build envelope: %w", err)
	}

	// Retrieve the user's GatewayEDI public key from the keyring.
	key, err := model.GetKeyringEntry(userName, GatewayEDIEligibilityKeyName)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: keyring entry '%s' not found for user '%s': %w",
			GatewayEDIEligibilityKeyName, userName, err)
	}

	// PGP-encrypt the SOAP request with the user's public key.
	encryptedPayload, err := crypto.EncryptPGP(envelope, key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: pgp encrypt: %w", err)
	}

	// Retrieve the service URI from user configuration.
	configs, err := model.GetConfigValues(userName)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: get config values: %w", err)
	}

	serviceUri := ""
	for _, cfg := range configs {
		if cfg.Option == "gatewayEdiServiceUri" {
			serviceUri = cfg.Value
			break
		}
	}

	// Stub: SOAP HTTP POST — not implemented yet.
	// When implemented, this would POST encryptedPayload to serviceUri
	// and parse the SOAP response.
	_ = encryptedPayload
	_ = serviceUri

	return &EligibilityResponse{
		Status:      StatusOK,
		SuccessCode: SuccessCodeSuccess,
		Messages:    []string{"GatewayEDI eligibility stub: success"},
	}, nil
}

// GetPluginName returns the Java-style dotted class name of this plugin.
func (g *GatewayEDIEligibility) GetPluginName() string {
	return GatewayEDIEligibilityClass
}

// GetPluginVersion returns the plugin version.
func (g *GatewayEDIEligibility) GetPluginVersion() string {
	return GatewayEDIEligibilityVersion
}

// GetPluginConfigurationOptions returns the names of user-configurable
// options required by this plugin.
func (g *GatewayEDIEligibility) GetPluginConfigurationOptions() []string {
	return []string{
		"gatewayEdiUsername",
		"gatewayEdiPassword",
		"gatewayEdiServiceUri",
	}
}

// SetContext stores the execution context for use by this plugin.
func (g *GatewayEDIEligibility) SetContext(ctx context.Context) error {
	g.ctx = ctx
	return nil
}
