package main

import (
	"encoding/json"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const (
	pluginID      = "cpa-auth-zai"
	pluginVersion = "0.1.0"
	providerID    = "zai"
)

// handler processes one RPC method payload and returns the marshaled result.
type handler func(payload []byte) (any, error)

var handlers = map[string]handler{}

func registerHandler(method string, h handler) {
	handlers[method] = h
}

// handleCall routes one plugin RPC method. It is the pure-Go seam used by the
// cgo exports in main.go and by tests.
func handleCall(method string, payload []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := parsePluginConfig(lifecycleConfigYAML(payload)); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(registration())
	case pluginabi.MethodPluginQuiesce, pluginabi.MethodPluginShutdown:
		return okEnvelope(map[string]any{})
	}
	h, ok := handlers[method]
	if !ok {
		return nil, fmt.Errorf("unknown method: %s", method)
	}
	result, errHandle := h(payload)
	if errHandle != nil {
		return nil, errHandle
	}
	return okEnvelope(result)
}

// registration reports the plugin identity and capabilities. Schema version 6
// keeps management JSON bodies unescaped and matches the inspected CPA v8 host.
func registration() map[string]any {
	return map[string]any{
		"schema_version": pluginabi.SchemaVersion,
		"metadata": map[string]any{
			"Name":             "Z.AI",
			"Version":          pluginVersion,
			"Author":           "cri",
			"GitHubRepository": "https://github.com/router-for-me/cpa-auth-zai",
			"Logo":             "https://z-cdn.chatglm.cn/z-ai/static/logo.svg",
			"ConfigFields": []map[string]any{{
				"Name":        "history-timezone",
				"Type":        pluginapi.ConfigFieldTypeString,
				"Default":     "",
				"Description": "IANA timezone for usage-history display when the request omits timezone; empty uses the provider's source timezone.",
			}, {
				"Name":        "api-key-name",
				"Type":        pluginapi.ConfigFieldTypeString,
				"Default":     "cpa-auth-zai",
				"Description": "Exact z.ai project API-key name to reuse or create during login.",
			}},
		},
		"capabilities": map[string]any{
			"auth_provider":           true,
			"model_provider":          true,
			"executor":                true,
			"executor_model_scope":    pluginapi.ExecutorModelScopeOAuth,
			"executor_input_formats":  []string{"chat-completions", "openai-responses", "anthropic"},
			"executor_output_formats": []string{"chat-completions", "openai-responses", "anthropic"},
			"quota_provider":          true,
			"management_api":          true,
		},
	}
}

func okEnvelope(result any) ([]byte, error) {
	raw, errMarshal := json.Marshal(result)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(pluginabi.Envelope{OK: true, Result: raw})
}

// errEnvelope builds a failed envelope. httpStatus maps upstream status codes
// into client-visible CPA errors per the plugin executor error contract.
func errEnvelope(code, message string, httpStatus int) []byte {
	raw, errMarshal := json.Marshal(pluginabi.Envelope{
		Error: pluginabi.NewError(code, message, httpStatus),
	})
	if errMarshal != nil {
		raw, _ = json.Marshal(pluginabi.Envelope{Error: pluginabi.NewError("plugin_error", errMarshal.Error(), 0)})
	}
	return raw
}
