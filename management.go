package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// usageHistoryRoutePath is relative to the host's /v0/management prefix, so
// the final URL is /v0/management/plugins/cpa-auth-zai/usage/history.
const usageHistoryRoutePath = "/plugins/cpa-auth-zai/usage/history"

// maxHistoryRange caps one history request at a leap year of source days.
const maxHistoryRange = 366 * 24 * time.Hour

func init() {
	registerHandler(pluginabi.MethodManagementRegister, handleManagementRegister)
	registerHandler(pluginabi.MethodManagementHandle, handleManagementHandle)
}

// handleManagementRegister declares the protected history route. Menu stays
// empty so the route is never exposed as an unauthenticated /v0/resource.
func handleManagementRegister([]byte) (any, error) {
	return map[string]any{
		"routes": []map[string]any{{
			"Method":      "GET",
			"Path":        usageHistoryRoutePath,
			"Menu":        "",
			"Description": "Z.ai token usage history (account-scoped)",
		}},
	}, nil
}

// managementWireRequest mirrors the host's ManagementRequest payload. The
// host marshals untagged Go fields, so keys are capitalized.
type managementWireRequest struct {
	Method         string              `json:"Method"`
	Path           string              `json:"Path"`
	Headers        map[string][]string `json:"Headers"`
	Query          map[string][]string `json:"Query"`
	Body           []byte              `json:"Body"`
	HostCallbackID string              `json:"host_callback_id,omitempty"`
}

// managementWireResponse mirrors the host's expected ManagementResponse shape
// (untagged Go fields, Body base64).
type managementWireResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       []byte              `json:"Body"`
}

func managementJSON(status int, payload any) managementWireResponse {
	body, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		body = []byte(`{"error":"failed to encode response"}`)
		status = http.StatusInternalServerError
	}
	return managementWireResponse{
		StatusCode: status,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       body,
	}
}

func managementError(status int, message string) managementWireResponse {
	return managementJSON(status, map[string]string{"error": message})
}

func handleManagementHandle(payload []byte) (any, error) {
	var request managementWireRequest
	if len(payload) > 0 {
		if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
			return nil, fmt.Errorf("decode management request: %w", errUnmarshal)
		}
	}
	if !strings.EqualFold(request.Method, http.MethodGet) || !strings.HasSuffix(request.Path, usageHistoryRoutePath) {
		return managementError(http.StatusNotFound, "not found"), nil
	}

	query := func(key string) string {
		if values := request.Query[key]; len(values) > 0 {
			return strings.TrimSpace(values[0])
		}
		return ""
	}
	authIndex := query("auth_index")
	if authIndex == "" {
		return managementError(http.StatusBadRequest, "auth_index is required"), nil
	}
	requestedZone := query("timezone")
	if requestedZone == "" {
		// Operator-level fallback (plugins.configs.cpa-auth-zai.history-timezone)
		// until the management UI gains a timezone preference.
		requestedZone = preferredTimezone()
	}
	if requestedZone != "" {
		if _, errLoad := time.LoadLocation(requestedZone); errLoad != nil {
			return managementError(http.StatusBadRequest, "unknown timezone"), nil
		}
	}
	start, errStart := time.Parse(time.RFC3339, query("start"))
	if errStart != nil {
		return managementError(http.StatusBadRequest, "start must be RFC3339"), nil
	}
	end, errEnd := time.Parse(time.RFC3339, query("end"))
	if errEnd != nil {
		return managementError(http.StatusBadRequest, "end must be RFC3339"), nil
	}
	if !end.After(start) {
		return managementError(http.StatusBadRequest, "end must be after start"), nil
	}
	if end.Sub(start) > maxHistoryRange {
		return managementError(http.StatusBadRequest, "range must not exceed 366 days"), nil
	}

	result, errGet := callHost(pluginabi.MethodHostAuthGet, pluginapi.HostAuthGetRequest{AuthIndex: authIndex})
	if errGet != nil {
		return managementError(http.StatusNotFound, "auth not found"), nil
	}
	var record pluginapi.HostAuthGetResponse
	if errUnmarshal := json.Unmarshal(result, &record); errUnmarshal != nil {
		return managementError(http.StatusNotFound, "auth not found"), nil
	}
	storage, errParse := parseZaiStorage(record.JSON)
	if errParse != nil {
		return managementError(http.StatusUnauthorized, "stored credentials are not usable"), nil
	}

	detail, errFetch := fetchUsageDetail(storage, request.HostCallbackID, start, end)
	if errFetch != nil {
		return managementError(http.StatusBadGateway, errFetch.Error()), nil
	}
	view, errView := buildUsageView(detail, UsageQuery{
		AuthIndex: authIndex,
		Start:     start,
		End:       end,
		Timezone:  requestedZone,
	})
	if errView != nil {
		return managementError(http.StatusBadRequest, errView.Error()), nil
	}
	return managementJSON(http.StatusOK, view), nil
}
