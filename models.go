package main

import (
	"encoding/json"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

func init() {
	registerHandler(pluginabi.MethodModelStatic, handleModelStatic)
	registerHandler(pluginabi.MethodModelForAuth, handleModelForAuth)
	registerHandler(pluginabi.MethodExecutorIdentifier, handleExecutorIdentifier)
}

// modelInfo mirrors the pluginapi.ModelInfo wire keys, which are the default Go
// field names. Capability fields stay unset until z.ai documents them.
type modelInfo struct {
	ID          string
	Object      string
	Created     int64
	OwnedBy     string
	DisplayName string
	Name        string
}

// modelResponse mirrors the pluginapi.ModelResponse wire keys.
type modelResponse struct {
	Provider string      `json:"Provider"`
	Models   []modelInfo `json:"Models"`
}

// codingPlanModels is the GLM Coding Plan catalog from the official z.ai
// devpack FAQ. It is never derived from usage history.
var codingPlanModels = []modelInfo{
	{ID: "glm-5.3", Object: "model", OwnedBy: providerID, DisplayName: "GLM-5.3", Name: "glm-5.3"},
	{ID: "glm-5.3-flash", Object: "model", OwnedBy: providerID, DisplayName: "GLM-5.3-Flash", Name: "glm-5.3-flash"},
}

// handleModelStatic returns an empty catalog: without account evidence the
// plugin makes no availability claim.
func handleModelStatic([]byte) (any, error) {
	return modelResponse{Provider: providerID, Models: []modelInfo{}}, nil
}

// handleModelForAuth validates the account record and returns the fixed Coding
// Plan catalog for it.
func handleModelForAuth(payload []byte) (any, error) {
	var request struct {
		AuthID      string `json:"AuthID"`
		StorageJSON []byte `json:"StorageJSON"`
	}
	if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
		return nil, fmt.Errorf("zai: decode model.for_auth request: %w", errUnmarshal)
	}
	if _, errParse := parseZaiStorage(request.StorageJSON); errParse != nil {
		return nil, errParse
	}
	return modelResponse{Provider: providerID, Models: append([]modelInfo(nil), codingPlanModels...)}, nil
}

// handleExecutorIdentifier returns the provider key CPA routes executors by.
// The host lowercases it and uses it as the executor's provider slot when no
// model-provider registration preempts it, so it must equal the "zai" auth
// provider key (internal/pluginhost/adapters_executors.go executorProvider).
func handleExecutorIdentifier([]byte) (any, error) {
	return map[string]string{"identifier": providerID}, nil
}
