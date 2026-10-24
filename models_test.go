package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// Acceptance: the static catalog is empty — without account evidence the
// plugin makes no availability claim.
func TestModelsStaticEmpty(t *testing.T) {
	result := testCallOK(t, pluginabi.MethodModelStatic, []byte("{}"))
	var response struct {
		Provider string `json:"Provider"`
		Models   []struct {
			ID string `json:"ID"`
		} `json:"Models"`
	}
	testDecode(t, result, &response)
	if response.Provider != providerID {
		t.Fatalf("Provider = %q, want %q", response.Provider, providerID)
	}
	if len(response.Models) != 0 {
		t.Fatalf("static models = %v, want empty", response.Models)
	}
}

// Acceptance: an authenticated account gets its complete catalog from z.ai,
// including models unknown to the plugin release.
func TestModelsForAuthLoadsProviderCatalog(t *testing.T) {
	testWithFakeHost(t, func(call hostCall, _ int) (any, error) {
		var request hostHTTPRequest
		if errUnmarshal := json.Unmarshal(call.Payload, &request); errUnmarshal != nil {
			t.Fatalf("decode host request: %v", errUnmarshal)
		}
		if request.Request.Method != http.MethodGet || request.Request.URL != "https://api.z.ai/api/coding/paas/v4/models" {
			t.Fatalf("upstream request = %s %s", request.Request.Method, request.Request.URL)
		}
		if got := request.Request.Headers["Authorization"]; !reflect.DeepEqual(got, []string{"Bearer tok-biz"}) {
			t.Fatalf("Authorization = %v", got)
		}
		return map[string]any{
			"StatusCode": http.StatusOK,
			"Body":       []byte(`{"object":"list","data":[{"id":"glm-5.3"},{"id":"glm-5.3-flash"},{"id":"glm-future"}]}`),
		}, nil
	})
	request := testJSON(t, map[string]any{
		"AuthID":           "zai-a-b-c",
		"AuthProvider":     providerID,
		"StorageJSON":      []byte(`{"version":1,"type":"zai","biz_access_token":"tok-biz","email":"a@b.c"}`),
		"host_callback_id": "cb-1",
	})
	result := testCallOK(t, pluginabi.MethodModelForAuth, request)
	var response modelResponse
	testDecode(t, result, &response)
	want := []modelInfo{
		{ID: "glm-5.3", Object: "model", OwnedBy: providerID, DisplayName: "glm-5.3", Name: "glm-5.3"},
		{ID: "glm-5.3-flash", Object: "model", OwnedBy: providerID, DisplayName: "glm-5.3-flash", Name: "glm-5.3-flash"},
		{ID: "glm-future", Object: "model", OwnedBy: providerID, DisplayName: "glm-future", Name: "glm-future"},
	}
	if response.Provider != providerID || !reflect.DeepEqual(response.Models, want) {
		t.Fatalf("response = %+v, want provider %q and models %+v", response, providerID, want)
	}
}

func TestModelsForAuthCatalogFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "rejected", status: http.StatusUnauthorized, body: `{"msg":"tok-biz rejected"}`},
		{name: "envelope rejection", status: http.StatusOK, body: `{"code":401,"msg":"tok-biz rejected"}`},
		{name: "empty", status: http.StatusOK, body: `{"data":[]}`},
		{name: "invalid", status: http.StatusOK, body: `not-json`},
	} {
		t.Run(test.name, func(t *testing.T) {
			testWithFakeHost(t, func(_ hostCall, _ int) (any, error) {
				return map[string]any{"StatusCode": test.status, "Body": []byte(test.body)}, nil
			})
			request := testJSON(t, map[string]any{
				"StorageJSON":      []byte(`{"version":1,"type":"zai","biz_access_token":"tok-biz"}`),
				"host_callback_id": "cb-1",
			})
			raw, errHandle := handleCall(pluginabi.MethodModelForAuth, request)
			if errHandle == nil {
				t.Fatalf("model.for_auth succeeded with a fixed fallback: %s", raw)
			}
			if strings.Contains(errHandle.Error(), "tok-biz") {
				t.Fatalf("error leaks credential: %v", errHandle)
			}
		})
	}
}

// Acceptance: undecodable, token-less, and non-business-token storage records
// reject model discovery.
func TestModelsForAuthRejectsBadStorage(t *testing.T) {
	for _, storage := range [][]byte{
		[]byte("not-json"),
		[]byte(`{"version":1,"type":"zai"}`),
		[]byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1"}`),
		[]byte(`{"version":1,"type":"other","oauth_access_token":"tok-1"}`),
	} {
		request := testJSON(t, map[string]any{
			"AuthProvider": providerID,
			"StorageJSON":  storage,
		})
		if _, errHandle := handleCall(pluginabi.MethodModelForAuth, request); errHandle == nil {
			t.Fatalf("model.for_auth accepted storage %s", storage)
		}
	}
}
