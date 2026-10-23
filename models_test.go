package main

import (
	"encoding/json"
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

// Acceptance: an authenticated account gets exactly the GLM Coding Plan
// catalog from the official z.ai devpack FAQ, with no invented capability
// fields and no models derived from usage history.
func TestModelsForAuthListsCodingPlanModels(t *testing.T) {
	request := testJSON(t, map[string]any{
		"AuthID":       "zai-a-b-c",
		"AuthProvider": providerID,
		"StorageJSON":  []byte(`{"version":1,"type":"zai","oauth_access_token":"tok-1","email":"a@b.c"}`),
	})
	result := testCallOK(t, pluginabi.MethodModelForAuth, request)
	var response struct {
		Provider string            `json:"Provider"`
		Models   []json.RawMessage `json:"Models"`
	}
	testDecode(t, result, &response)
	if response.Provider != providerID {
		t.Fatalf("Provider = %q, want %q", response.Provider, providerID)
	}
	if len(response.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(response.Models))
	}

	want := []struct{ id, displayName string }{
		{"glm-5.3", "GLM-5.3"},
		{"glm-5.3-flash", "GLM-5.3-Flash"},
	}
	for i, modelRaw := range response.Models {
		var model map[string]json.RawMessage
		testDecode(t, modelRaw, &model)
		var info struct {
			ID                  string `json:"ID"`
			Object              string `json:"Object"`
			Created             int64  `json:"Created"`
			OwnedBy             string `json:"OwnedBy"`
			DisplayName         string `json:"DisplayName"`
			Name                string `json:"Name"`
			ContextLength       int64  `json:"ContextLength"`
			MaxCompletionTokens int64  `json:"MaxCompletionTokens"`
		}
		testDecode(t, modelRaw, &info)
		if info.ID != want[i].id || info.DisplayName != want[i].displayName {
			t.Fatalf("model %d = %s/%s, want %s/%s", i, info.ID, info.DisplayName, want[i].id, want[i].displayName)
		}
		if info.Object != "model" || info.OwnedBy != providerID {
			t.Fatalf("model %s = object %q ownedBy %q", info.ID, info.Object, info.OwnedBy)
		}
		if info.Name != info.ID {
			t.Fatalf("model %s name = %q, want the model id", info.ID, info.Name)
		}
		if info.Created != 0 {
			t.Fatalf("model %s created = %d, want 0", info.ID, info.Created)
		}
		for _, key := range []string{
			"ContextLength", "MaxCompletionTokens", "InputTokenLimit", "OutputTokenLimit",
			"Thinking", "SupportedInputModalities", "SupportedOutputModalities",
		} {
			if _, ok := model[key]; ok {
				t.Fatalf("model %s invents %s", info.ID, key)
			}
		}
	}
}

// Acceptance: undecodable or token-less storage records reject model discovery.
func TestModelsForAuthRejectsBadStorage(t *testing.T) {
	for _, storage := range [][]byte{
		[]byte("not-json"),
		[]byte(`{"version":1,"type":"zai"}`),
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
