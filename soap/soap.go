// Package soap provides a SOAP 1.1 compatibility layer that mirrors the
// Apache CXF JAX-WS interface from the old Java REMITT 0.5.x server. It
// intercepts POST requests to /services/interface, parses SOAP envelopes,
// dispatches to the existing Go REST handlers, and wraps responses in SOAP.
package soap

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/freemed/remitt-server/common"
	"github.com/labstack/echo/v5"
)

// Target namespace matching old Java @WebService annotation
const (
	TargetNamespace = "http://server.remitt.org/"
	ServicePath     = "/services/interface"
)

// ---------------------------------------------------------------------------
// SOAP 1.1 XML types
// ---------------------------------------------------------------------------

// Envelope is a SOAP 1.1 envelope.
type Envelope struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Body    Body     `xml:"http://schemas.xmlsoap.org/soap/envelope/ Body"`
}

// Body holds the operation request or response.
type Body struct {
	InnerXML []byte `xml:",innerxml"`
}

// Fault represents a SOAP fault.
type Fault struct {
	XMLName     xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Fault"`
	FaultCode   string   `xml:"faultcode"`
	FaultString string   `xml:"faultstring"`
}

// newFaultEnvelope builds a SOAP envelope containing a fault.
func newFaultEnvelope(code, message string) Envelope {
	return Envelope{
		Body: Body{
			InnerXML: marshalXML(Fault{
				FaultCode:   code,
				FaultString: message,
			}),
		},
	}
}

// ---------------------------------------------------------------------------
// Operation handler type
// ---------------------------------------------------------------------------

// Handler receives a parsed operation name, extracts typed parameters from
// inner XML, and returns XML response bytes (the SOAP body content).
type Handler func(c *echo.Context, innerXML []byte, username string) ([]byte, error)

// dispatchTable maps SOAP operation names (from the old Java WSDL) to handlers.
var dispatchTable = map[string]Handler{
	"getProtocolVersion":    handleGetProtocolVersion,
	"changePassword":        handleChangePassword,
	"getCurrentUserName":    handleGetCurrentUserName,
	"insertPayload":         handleInsertPayload,
	"resubmitPayload":       handleResubmitPayload,
	"getConfigValues":       handleGetConfigValues,
	"setConfigValue":        handleSetConfigValue,
	"getStatus":             handleGetStatus,
	"getBulkStatus":         handleGetBulkStatus,
	"getPlugins":            handleGetPlugins,
	"getFile":               handleGetFile,
	"getPluginOptions":      handleGetPluginOptions,
	"getFileList":           handleGetFileList,
	"getOutputMonths":       handleGetOutputMonths,
	"getOutputYears":        handleGetOutputYears,
	"getEligibility":        handleGetEligibility,
	"batchEligibilityCheck": handleBatchEligibilityCheck,
	"parseData":             handleParseData,
	"addKeyToKeyring":       handleAddKeyToKeyring,
	"addRemittUser":         handleAddRemittUser,
	"listRemittUsers":       handleListRemittUsers,
	"validatePayload":       handleValidatePayload,
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// Middleware returns an Echo middleware that intercepts SOAP requests.
// It must be placed AFTER BasicAuth and LoadUserMiddleware so
// c.Get(common.AuthUserKey) returns the authenticated username.
func Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// Only handle requests to the SOAP service path
			if !strings.HasPrefix(c.Request().URL.Path, ServicePath) {
				return next(c)
			}

			// Read request body
			body, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return writeSOAPFault(c, "Server", "Failed to read request body")
			}

			// Parse SOAP envelope
			var env Envelope
			if err := xml.Unmarshal(body, &env); err != nil {
				return writeSOAPFault(c, "Client", fmt.Sprintf("Invalid SOAP envelope: %v", err))
			}

			// Extract operation name from inner XML
			operation := extractOperation(env.Body.InnerXML)
			if operation == "" {
				return writeSOAPFault(c, "Client", "No operation found in SOAP body")
			}

			// Look up handler
			handler, ok := dispatchTable[operation]
			if !ok {
				return writeSOAPFault(c, "Client", fmt.Sprintf("Unknown operation: %s", operation))
			}

			// Get username from context (set by LoadUserMiddleware)
			username, _ := c.Get(common.AuthUserKey).(string)

			// Dispatch
			responseXML, err := handler(c, env.Body.InnerXML, username)
			if err != nil {
				return writeSOAPFault(c, "Server", err.Error())
			}

			// Build response envelope
			respEnv := Envelope{
				Body: Body{InnerXML: responseXML},
			}
			respBytes := append([]byte(xml.Header), marshalXML(respEnv)...)

			c.Response().Header().Set("Content-Type", "text/xml; charset=utf-8")
			c.Response().WriteHeader(http.StatusOK)
			_, writeErr := c.Response().Write(respBytes)
			return writeErr
		}
	}
}

// writeSOAPFault writes a SOAP fault response.
func writeSOAPFault(c *echo.Context, code, message string) error {
	faultEnv := newFaultEnvelope("SOAP-ENV:"+code, message)
	respBytes := append([]byte(xml.Header), marshalXML(faultEnv)...)
	c.Response().Header().Set("Content-Type", "text/xml; charset=utf-8")
	c.Response().WriteHeader(http.StatusInternalServerError)
	_, err := c.Response().Write(respBytes)
	return err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractOperation finds the operation name from the SOAP body inner XML.
// In CXF JAX-WS, the operation is a child element named after the method,
// with the namespace http://server.remitt.org/.
func extractOperation(innerXML []byte) string {
	decoder := xml.NewDecoder(strings.NewReader(string(innerXML)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			// Strip namespace prefix from element name
			name := se.Name.Local
			if name != "" {
				return name
			}
			// Check for "return" (SOAP response, not request)
			if name == "return" {
				return ""
			}
		}
	}
	return ""
}

// marshalXML marshals a value to XML bytes, returning empty on error.
func marshalXML(v any) []byte {
	data, err := xml.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return data
}

// xmlText extracts the text content of the first child element with the
// given local name from inner XML.
func xmlText(innerXML []byte, localName string) string {
	decoder := xml.NewDecoder(strings.NewReader(string(innerXML)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == localName {
			var content string
			if err := decoder.DecodeElement(&content, &se); err == nil {
				return content
			}
		}
	}
	return ""
}

// xmlTextAll extracts text content of all child elements with the given
// local name from inner XML.
func xmlTextAll(innerXML []byte, localName string) []string {
	var result []string
	decoder := xml.NewDecoder(strings.NewReader(string(innerXML)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == localName {
			var content string
			if err := decoder.DecodeElement(&content, &se); err == nil {
				result = append(result, content)
			}
		}
	}
	return result
}
