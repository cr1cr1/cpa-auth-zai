package main

import (
	"encoding/json"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// pluginSettings holds the plugin's own config from
// plugins.configs.cpa-auth-zai, delivered via plugin.register/reconfigure.
type pluginSettings struct {
	HistoryTimezone string `yaml:"history-timezone"`
	APIKeyName      string `yaml:"api-key-name"`
}

var settingsMu sync.RWMutex
var settings pluginSettings

// parsePluginConfig stores a decoded settings snapshot. Unknown keys are
// ignored; validation of the timezone happens when it is used so a bad value
// surfaces as a request error instead of a failed plugin load.
func parsePluginConfig(raw []byte) error {
	var next pluginSettings
	if len(raw) > 0 {
		if errUnmarshal := yaml.Unmarshal(raw, &next); errUnmarshal != nil {
			return errUnmarshal
		}
	}
	settingsMu.Lock()
	settings = next
	settingsMu.Unlock()
	return nil
}

// preferredTimezone returns the configured display timezone, if any.
func preferredTimezone() string {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	return settings.HistoryTimezone
}

func preferredAPIKeyName() string {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	if name := strings.TrimSpace(settings.APIKeyName); name != "" {
		return name
	}
	return "cpa-auth-zai"
}

// lifecycleConfigYAML extracts config_yaml from a register/reconfigure payload.
func lifecycleConfigYAML(payload []byte) []byte {
	var req struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	if errUnmarshal := json.Unmarshal(payload, &req); errUnmarshal != nil {
		return nil
	}
	return req.ConfigYAML
}
