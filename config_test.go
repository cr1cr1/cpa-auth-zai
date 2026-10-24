package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// Acceptance: the plugin's own config supplies the history display timezone
// when the request omits it, and the field is declared for the management UI.
func TestPluginConfigHistoryTimezone(t *testing.T) {
	t.Cleanup(func() { _ = parsePluginConfig(nil) })
	payload, errMarshal := json.Marshal(map[string][]byte{"config_yaml": []byte("history-timezone: Europe/Bucharest\n")})
	if errMarshal != nil {
		t.Fatalf("marshal: %v", errMarshal)
	}
	if _, errHandle := handleCall("plugin.reconfigure", payload); errHandle != nil {
		t.Fatalf("reconfigure failed: %v", errHandle)
	}
	if got := preferredTimezone(); got != "Europe/Bucharest" {
		t.Fatalf("preferredTimezone = %q", got)
	}

	reg, errHandle := handleCall("plugin.register", payload)
	if errHandle != nil {
		t.Fatalf("register failed: %v", errHandle)
	}
	var registration struct {
		Result struct {
			Metadata struct {
				ConfigFields []struct {
					Name string `json:"Name"`
					Type string `json:"Type"`
				} `json:"ConfigFields"`
			} `json:"metadata"`
		} `json:"result"`
	}
	if errUnmarshal := json.Unmarshal(reg, &registration); errUnmarshal != nil {
		t.Fatalf("decode registration: %v", errUnmarshal)
	}
	found := false
	for _, field := range registration.Result.Metadata.ConfigFields {
		if field.Name == "history-timezone" {
			found = field.Type == "string"
		}
	}
	if !found {
		t.Fatalf("history-timezone config field not declared: %s", reg)
	}
}

func TestPluginConfigAPIKeyName(t *testing.T) {
	t.Cleanup(func() { _ = parsePluginConfig(nil) })
	if got := preferredAPIKeyName(); got != "cpa-auth-zai" {
		t.Fatalf("default API key name = %q", got)
	}
	payload, errMarshal := json.Marshal(map[string][]byte{"config_yaml": []byte("api-key-name:  team-cpa  \n")})
	if errMarshal != nil {
		t.Fatalf("marshal: %v", errMarshal)
	}
	reg, errHandle := handleCall("plugin.register", payload)
	if errHandle != nil {
		t.Fatalf("register failed: %v", errHandle)
	}
	if got := preferredAPIKeyName(); got != "team-cpa" {
		t.Fatalf("configured API key name = %q", got)
	}
	var envelope struct {
		Result struct {
			Metadata struct {
				ConfigFields []struct {
					Name    string `json:"Name"`
					Type    string `json:"Type"`
					Default string `json:"Default"`
				} `json:"ConfigFields"`
			} `json:"metadata"`
		} `json:"result"`
	}
	if errUnmarshal := json.Unmarshal(reg, &envelope); errUnmarshal != nil {
		t.Fatalf("decode registration: %v", errUnmarshal)
	}
	for _, field := range envelope.Result.Metadata.ConfigFields {
		if field.Name == "api-key-name" {
			if field.Type != "string" || field.Default != "cpa-auth-zai" {
				t.Fatalf("api-key-name field = %+v", field)
			}
			return
		}
	}
	t.Fatalf("api-key-name config field not declared: %s", reg)
}

// Acceptance: a broken configured timezone surfaces as a history request
// error — never a silent fallback to the server timezone.
func TestPluginConfigInvalidTimezoneFailsRequest(t *testing.T) {
	t.Cleanup(func() { _ = parsePluginConfig(nil) })
	bad, errMarshal := json.Marshal(map[string][]byte{"config_yaml": []byte("history-timezone: Not/AZone\n")})
	if errMarshal != nil {
		t.Fatalf("marshal: %v", errMarshal)
	}
	if _, errHandle := handleCall("plugin.reconfigure", bad); errHandle != nil {
		t.Fatalf("reconfigure with invalid zone must still load: %v", errHandle)
	}
	raw, errHandle := handleCall(pluginabi.MethodManagementHandle, mgmtHistoryPayload(t, map[string][]string{"auth_index": {"idx"}}))
	if errHandle != nil {
		t.Fatalf("management.handle failed: %v", errHandle)
	}
	status, body := mgmtResult(t, raw)
	if status != 400 || !strings.Contains(string(body), "unknown timezone") {
		t.Fatalf("invalid configured timezone: status=%d body=%s", status, body)
	}
}
