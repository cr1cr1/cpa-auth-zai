package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

func decodeModelCatalog(body []byte) ([]modelInfo, error) {
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(body, &catalog); errUnmarshal != nil {
		return nil, fmt.Errorf("zai: decode model catalog: %w", errUnmarshal)
	}
	models := make([]modelInfo, 0, len(catalog.Data))
	for _, entry := range catalog.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		models = append(models, modelInfo{ID: id, Object: "model", OwnedBy: providerID, DisplayName: id, Name: id})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("zai: model catalog has no models")
	}
	return models, nil
}

// handleModelStatic returns an empty catalog: without account evidence the
// plugin makes no availability claim.
func handleModelStatic([]byte) (any, error) {
	return modelResponse{Provider: providerID, Models: []modelInfo{}}, nil
}

// handleModelForAuth loads the account catalog with the business token derived
// from OAuth login. It has no fixed-model fallback.
func handleModelForAuth(payload []byte) (any, error) {
	var request struct {
		AuthID         string `json:"AuthID"`
		StorageJSON    []byte `json:"StorageJSON"`
		HostCallbackID string `json:"host_callback_id"`
	}
	if errUnmarshal := json.Unmarshal(payload, &request); errUnmarshal != nil {
		return nil, fmt.Errorf("zai: decode model.for_auth request: %w", errUnmarshal)
	}
	storage, errParse := parseZaiStorage(request.StorageJSON)
	if errParse != nil {
		return nil, errParse
	}
	token := strings.TrimSpace(storage.BizAccessToken)
	if token == "" {
		return nil, fmt.Errorf("zai: business access token is required for model discovery")
	}
	response, errDo := hostDo(request.HostCallbackID, http.MethodGet, codingModelsURL,
		map[string][]string{"Accept": {"application/json"}, "Authorization": {"Bearer " + token}}, nil)
	if errDo != nil {
		return nil, errDo
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, upstreamError(response.StatusCode, response.Body, token)
	}
	if envelopeAuthRejected(response.Body) {
		return nil, upstreamError(http.StatusUnauthorized, response.Body, token)
	}
	models, errDecode := decodeModelCatalog(response.Body)
	if errDecode != nil {
		return nil, errDecode
	}
	return modelResponse{Provider: providerID, Models: models}, nil
}

// handleExecutorIdentifier returns the provider key CPA routes executors by.
// The host lowercases it and uses it as the executor's provider slot when no
// model-provider registration preempts it, so it must equal the "zai" auth
// provider key (internal/pluginhost/adapters_executors.go executorProvider).
func handleExecutorIdentifier([]byte) (any, error) {
	return map[string]string{"identifier": providerID}, nil
}
