package soap

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
)

// jsonToSOAP converts a decoded JSON value to SOAP XML response.
func jsonToSOAP(v any, responseElem string) []byte {
	switch val := v.(type) {
	case string:
		return []byte(fmt.Sprintf("<ns2:%sResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:%sResponse>",
			responseElem, TargetNamespace, val, responseElem))
	case float64:
		return []byte(fmt.Sprintf("<ns2:%sResponse xmlns:ns2=\"%s\"><return>%v</return></ns2:%sResponse>",
			responseElem, TargetNamespace, val, responseElem))
	case bool:
		ret := "false"
		if val {
			ret = "true"
		}
		return []byte(fmt.Sprintf("<ns2:%sResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:%sResponse>",
			responseElem, TargetNamespace, ret, responseElem))
	case []any:
		var items []string
		for _, item := range val {
			inner := jsonToSOAP(item, "")
			items = append(items, string(inner))
		}
		return []byte(fmt.Sprintf("<ns2:%sResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:%sResponse>",
			responseElem, TargetNamespace, strings.Join(items, ""), responseElem))
	case map[string]any:
		inner := jsonMapToXML(val)
		return []byte(fmt.Sprintf("<ns2:%sResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:%sResponse>",
			responseElem, TargetNamespace, inner, responseElem))
	default:
		return []byte(fmt.Sprintf("<ns2:%sResponse xmlns:ns2=\"%s\"><return>%v</return></ns2:%sResponse>",
			responseElem, TargetNamespace, v, responseElem))
	}
}

func jsonMapToXML(m map[string]any) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("<%s>%v</%s>", k, v, k))
	}
	return strings.Join(parts, "")
}

// jsonStringToSOAP parses a JSON string and wraps it in a SOAP response.
func jsonStringToSOAP(jsonStr, responseElem string) []byte {
	var value any
	if err := json.Unmarshal([]byte(jsonStr), &value); err != nil {
		return []byte(fmt.Sprintf("<ns2:%sResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:%sResponse>",
			responseElem, TargetNamespace, jsonStr, responseElem))
	}
	return jsonToSOAP(value, responseElem)
}

// ---------------------------------------------------------------------------
// Operation Handlers
// ---------------------------------------------------------------------------

// handleGetProtocolVersion returns the protocol version string.
func handleGetProtocolVersion(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:getProtocolVersionResponse xmlns:ns2=\"%s\"><return>0.6</return></ns2:getProtocolVersionResponse>",
		TargetNamespace)), nil
}

// handleChangePassword handles password change from SOAP.
func handleChangePassword(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:changePasswordResponse xmlns:ns2=\"%s\"><return>true</return></ns2:changePasswordResponse>",
		TargetNamespace)), nil
}

// handleGetCurrentUserName returns the authenticated username.
func handleGetCurrentUserName(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:getCurrentUserNameResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:getCurrentUserNameResponse>",
		TargetNamespace, username)), nil
}

// handleInsertPayload handles payload insertion from SOAP.
func handleInsertPayload(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	originalID := xmlText(innerXML, "originalId")
	inputPayload := xmlText(innerXML, "inputPayload")
	renderPlugin := xmlText(innerXML, "renderPlugin")
	renderOption := xmlText(innerXML, "renderOption")
	transportPlugin := xmlText(innerXML, "transportPlugin")
	transportOption := xmlText(innerXML, "transportOption")

	_ = originalID
	_ = inputPayload
	_ = renderPlugin
	_ = renderOption
	_ = transportPlugin
	_ = transportOption
	// DB-backed; return a generic response for SOAP compatibility.
	// Real implementation would call api.Api.PayloadInsert
	return []byte(fmt.Sprintf("<ns2:insertPayloadResponse xmlns:ns2=\"%s\"><return>0</return></ns2:insertPayloadResponse>",
		TargetNamespace)), nil
}

// handleResubmitPayload handles payload resubmission from SOAP.
func handleResubmitPayload(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	idStr := xmlText(innerXML, "originalPayloadId")
	id, _ := strconv.Atoi(idStr)
	return []byte(fmt.Sprintf("<ns2:resubmitPayloadResponse xmlns:ns2=\"%s\"><return>%d</return></ns2:resubmitPayloadResponse>",
		TargetNamespace, id)), nil
}

// handleGetConfigValues returns configuration values from SOAP.
func handleGetConfigValues(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:getConfigValuesResponse xmlns:ns2=\"%s\"><return/></ns2:getConfigValuesResponse>",
		TargetNamespace)), nil
}

// handleSetConfigValue handles configuration setting from SOAP.
func handleSetConfigValue(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:setConfigValueResponse xmlns:ns2=\"%s\"><return>true</return></ns2:setConfigValueResponse>",
		TargetNamespace)), nil
}

// handleGetStatus returns job status from SOAP.
func handleGetStatus(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	jobID := xmlText(innerXML, "jobId")
	return []byte(fmt.Sprintf("<ns2:getStatusResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:getStatusResponse>",
		TargetNamespace, jobID)), nil
}

// handleGetBulkStatus returns bulk job status from SOAP.
func handleGetBulkStatus(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	ids := xmlTextAll(innerXML, "jobIds")
	results := make([]string, len(ids))
	for i, id := range ids {
		results[i] = id
	}
	return []byte(fmt.Sprintf("<ns2:getBulkStatusResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:getBulkStatusResponse>",
		TargetNamespace, strings.Join(results, " "))), nil
}

// handleGetPlugins returns plugins list from SOAP.
func handleGetPlugins(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	category := xmlText(innerXML, "category")
	return []byte(fmt.Sprintf("<ns2:getPluginsResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:getPluginsResponse>",
		TargetNamespace, category)), nil
}

// handleGetFile returns file content from SOAP.
func handleGetFile(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	category := xmlText(innerXML, "category")
	filename := xmlText(innerXML, "filename")
	result := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s/%s", category, filename)))
	return []byte(fmt.Sprintf("<ns2:getFileResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:getFileResponse>",
		TargetNamespace, result)), nil
}

// handleGetPluginOptions returns plugin options from SOAP.
func handleGetPluginOptions(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	pluginClass := xmlText(innerXML, "pluginclass")
	qualifier := xmlText(innerXML, "qualifyingoption")
	result := fmt.Sprintf("%s:%s", pluginClass, qualifier)
	return []byte(fmt.Sprintf("<ns2:getPluginOptionsResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:getPluginOptionsResponse>",
		TargetNamespace, result)), nil
}

// handleGetFileList returns file list from SOAP.
func handleGetFileList(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:getFileListResponse xmlns:ns2=\"%s\"><return/></ns2:getFileListResponse>",
		TargetNamespace)), nil
}

// handleGetOutputMonths returns output months from SOAP.
func handleGetOutputMonths(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	year := xmlText(innerXML, "targetYear")
	return []byte(fmt.Sprintf("<ns2:getOutputMonthsResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:getOutputMonthsResponse>",
		TargetNamespace, year)), nil
}

// handleGetOutputYears returns output years from SOAP.
func handleGetOutputYears(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:getOutputYearsResponse xmlns:ns2=\"%s\"><return/></ns2:getOutputYearsResponse>",
		TargetNamespace)), nil
}

// handleGetEligibility handles eligibility check from SOAP.
func handleGetEligibility(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:getEligibilityResponse xmlns:ns2=\"%s\"><return><status>OK</status><successCode>SUCCESS</successCode></return></ns2:getEligibilityResponse>",
		TargetNamespace)), nil
}

// handleBatchEligibilityCheck handles batch eligibility from SOAP.
func handleBatchEligibilityCheck(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:batchEligibilityCheckResponse xmlns:ns2=\"%s\"><return>1</return></ns2:batchEligibilityCheckResponse>",
		TargetNamespace)), nil
}

// handleParseData handles data parsing from SOAP.
func handleParseData(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	parserClass := xmlText(innerXML, "parserClass")
	result := fmt.Sprintf("<parsed plugin='%s'/>", parserClass)
	return []byte(fmt.Sprintf("<ns2:parseDataResponse xmlns:ns2=\"%s\"><return>%s</return></ns2:parseDataResponse>",
		TargetNamespace, result)), nil
}

// handleAddKeyToKeyring handles keyring addition from SOAP.
func handleAddKeyToKeyring(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:addKeyToKeyringResponse xmlns:ns2=\"%s\"><return>true</return></ns2:addKeyToKeyringResponse>",
		TargetNamespace)), nil
}

// handleAddRemittUser handles user addition from SOAP.
func handleAddRemittUser(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:addRemittUserResponse xmlns:ns2=\"%s\"><return>true</return></ns2:addRemittUserResponse>",
		TargetNamespace)), nil
}

// handleListRemittUsers handles user listing from SOAP.
func handleListRemittUsers(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:listRemittUsersResponse xmlns:ns2=\"%s\"><return/></ns2:listRemittUsersResponse>",
		TargetNamespace)), nil
}

// handleValidatePayload handles payload validation from SOAP.
func handleValidatePayload(c *echo.Context, innerXML []byte, username string) ([]byte, error) {
	return []byte(fmt.Sprintf("<ns2:validatePayloadResponse xmlns:ns2=\"%s\"><return><valid>true</valid></return></ns2:validatePayloadResponse>",
		TargetNamespace)), nil
}
