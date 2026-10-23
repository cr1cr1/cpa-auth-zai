package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

func init() {
	registerHandler(pluginabi.MethodQuotaIdentifier, handleQuotaIdentifier)
	registerHandler(pluginabi.MethodQuotaDescribe, handleQuotaDescribe)
	registerHandler(pluginabi.MethodQuotaFetch, handleQuotaFetch)
	registerHandler(pluginabi.MethodQuotaReset, handleQuotaReset)
}

// quotaRequest is the wire shape of quota.* RPC payloads. StorageJSON is
// base64-decoded by encoding/json because the field is []byte.
type quotaRequest struct {
	AuthIndex      string            `json:"auth_index"`
	AuthID         string            `json:"auth_id"`
	Provider       string            `json:"provider"`
	StorageJSON    []byte            `json:"storage_json,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
	Host           json.RawMessage   `json:"host,omitempty"`
	HostCallbackID string            `json:"host_callback_id,omitempty"`
}

func handleQuotaIdentifier([]byte) (any, error) {
	return map[string]any{"identifier": providerID}, nil
}

func handleQuotaDescribe([]byte) (any, error) {
	return map[string]any{
		"supported_providers": []string{providerID},
		"display_name":        "Z.ai",
		"supports_reset":      false,
	}, nil
}

// handleQuotaFetch returns only measured quota. Any failure — missing
// credentials, rejected tokens, non-2xx, undecodable body — yields an empty
// successful response: missing quota is unknown, never zero-filled.
func handleQuotaFetch(payload []byte) (any, error) {
	var request quotaRequest
	if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
		return map[string]any{}, nil
	}
	storage, errParse := parseZaiStorage(request.StorageJSON)
	if errParse != nil {
		// CPA's credential quota path does not embed storage_json; fall back
		// to the host auth record before treating quota as unknown.
		if request.AuthIndex != "" {
			storage, errParse = resolveStorageByIndex(request.AuthIndex)
		}
		if errParse != nil {
			return map[string]any{}, nil
		}
	}
	response, okFetch := tryBearerCandidates(request.HostCallbackID, quotaLimitURL, storage)
	if !okFetch {
		return map[string]any{}, nil
	}
	result, errBuild := buildQuotaResponse(response.Body)
	if errBuild != nil {
		return map[string]any{}, nil
	}
	return result, nil
}

func handleQuotaReset([]byte) (any, error) {
	return nil, pluginabi.NewError("unsupported_operation", "z.ai quota resets are managed by the provider", 501)
}

// tryBearerCandidates GETs url once per bearer candidate, advancing only on
// upstream 401/403, and stops at the first 2xx. ok=false means the endpoint
// failed entirely; resp is non-nil when a non-2xx upstream answer caused it.
func tryBearerCandidates(callbackID, url string, storage *zaiStorage) (*hostHTTPResponse, bool) {
	for _, token := range storage.bearerCandidates() {
		response, errCall := hostDo(context.Background(), callbackID, "GET", url,
			map[string][]string{"Authorization": {"Bearer " + token}}, nil)
		if errCall != nil {
			return nil, false
		}
		switch {
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
			continue
		case response.StatusCode >= 200 && response.StatusCode < 300:
			return response, true
		default:
			return response, false
		}
	}
	return nil, false
}

// quotaLimitRow keeps every upstream value as json.Number so numbers may
// arrive as strings or JSON numbers. All fields are optional.
type quotaLimitRow struct {
	Unit            json.Number `json:"unit"`
	Number          json.Number `json:"number"`
	Usage           json.Number `json:"usage"`
	Percentage      json.Number `json:"percentage"`
	CurrentValue    string      `json:"currentValue"`
	AltCurrentValue string      `json:"current_value"`
	NextResetTime   json.Number `json:"nextResetTime"`
	AltNextReset    json.Number `json:"next_reset_time"`
	Limit           json.Number `json:"limit"`
}

// quotaWindow maps a verified unit/number pair to a bucket window. Other
// pairs are ignored, never mislabeled.
func quotaWindow(unit, number string) string {
	switch {
	case unit == "3" && number == "5":
		return "5h"
	case unit == "6" && number == "1":
		return "7d"
	default:
		return ""
	}
}

func quotaWindowDescription(window string) string {
	if window == "5h" {
		return "Rolling 5-hour window"
	}
	return "Rolling 7-day window"
}

// buildQuotaResponse decodes a 2xx quota/limit body into the
// pluginapi.QuotaFetchResponse JSON shape, emitting only known values.
func buildQuotaResponse(body []byte) (map[string]any, error) {
	var envelope struct {
		Code    json.Number `json:"code"`
		Success *bool       `json:"success"`
		Msg     string      `json:"msg"`
		Data    struct {
			SubscriptionStatus    string          `json:"subscriptionStatus"`
			AltSubscriptionStatus string          `json:"subscription_status"`
			Limits                []quotaLimitRow `json:"limits"`
		} `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(body, &envelope); errUnmarshal != nil {
		return nil, fmt.Errorf("zai: decode quota limit: %w", errUnmarshal)
	}
	if envelope.Code.String() != "200" || (envelope.Success != nil && !*envelope.Success) {
		return nil, fmt.Errorf("zai: quota error %s: %s", envelope.Code.String(), envelope.Msg)
	}
	result := map[string]any{}
	plan := envelope.Data.SubscriptionStatus
	if plan == "" {
		plan = envelope.Data.AltSubscriptionStatus
	}
	if plan != "" {
		result["subscription"] = map[string]any{"plan": plan}
	}
	var metrics []map[string]any
	var buckets []map[string]any
	for _, row := range envelope.Data.Limits {
		window := quotaWindow(row.Unit.String(), row.Number.String())
		if window == "" {
			continue
		}
		percentage, hasPercentage := quotaNumber(row.Percentage)
		if hasPercentage {
			metrics = append(metrics, map[string]any{
				"key":    "usage_" + window,
				"label":  quotaMetricLabel(window),
				"value":  percentage,
				"format": "number",
			})
		}
		bucket := map[string]any{
			"window":      window,
			"description": quotaWindowDescription(window),
		}
		if hasPercentage {
			bucket["remainingFraction"] = math.Max(0, math.Min(1, 1-percentage/100))
		}
		if resetTime := quotaResetTime(row); resetTime != "" {
			bucket["resetTime"] = resetTime
		}
		buckets = append(buckets, bucket)
	}
	if len(metrics) > 0 {
		result["summary"] = metrics
	}
	if len(buckets) > 0 {
		result["groups"] = []map[string]any{{"displayName": "Usage windows", "buckets": buckets}}
	}
	return result, nil
}

func quotaMetricLabel(window string) string {
	if window == "5h" {
		return "5-hour usage %"
	}
	return "7-day usage %"
}

// quotaNumber parses an optional json.Number payload value into float64.
func quotaNumber(value json.Number) (float64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, errParse := strconv.ParseFloat(value.String(), 64)
	if errParse != nil {
		return 0, false
	}
	return parsed, true
}

// quotaResetTime renders a nextResetTime epoch-ms value as RFC3339 when it is
// present and positive. A reset instant is never fabricated.
func quotaResetTime(row quotaLimitRow) string {
	resetMs, hasReset := quotaNumber(row.NextResetTime)
	if !hasReset {
		resetMs, hasReset = quotaNumber(row.AltNextReset)
	}
	if !hasReset || resetMs <= 0 {
		return ""
	}
	return time.UnixMilli(int64(resetMs)).UTC().Format(time.RFC3339)
}
