package main

import (
	"crypto/rand"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	zcodeAppVersion          = "3.10.2"
	zcodeNodeMajor           = "24"
	zcodeOpenAISDKVersion    = "4.0.39"
	zcodeAnthropicSDKVersion = "4.0.27"
)

var (
	zcodeSessionID = sync.OnceValues(newZCodeUUID)
	zcodeOSVersion = sync.OnceValue(detectZCodeOSVersion)
)

func addZCodeFingerprint(headers map[string][]string, format string) error {
	sessionID, errSession := zcodeSessionID()
	if errSession != nil {
		return fmt.Errorf("generate ZCode session ID: %w", errSession)
	}
	queryID, errQuery := newZCodeUUID()
	if errQuery != nil {
		return fmt.Errorf("generate ZCode query ID: %w", errQuery)
	}
	requestID, errRequest := newZCodeUUID()
	if errRequest != nil {
		return fmt.Errorf("generate ZCode request ID: %w", errRequest)
	}
	traceID, errTrace := newZCodeUUID()
	if errTrace != nil {
		return fmt.Errorf("generate ZCode trace ID: %w", errTrace)
	}
	sdkVersion := zcodeOpenAISDKVersion
	if format == "claude" {
		sdkVersion = zcodeAnthropicSDKVersion
	}
	fingerprint := map[string]string{
		"User-Agent":           "ZCode/" + zcodeAppVersion + " ai-sdk/provider-utils/" + sdkVersion + " runtime/node.js/" + zcodeNodeMajor,
		"HTTP-Referer":         "https://zcode.z.ai",
		"X-Title":              "Z Code@electron",
		"X-ZCode-App-Version":  zcodeAppVersion,
		"X-ZCode-Agent":        "glm",
		"X-ZCode-Session-Type": "main",
		"X-Platform":           zcodePlatform(),
		"X-Os-Category":        zcodeOSCategory(),
		"X-Os-Version":         zcodeOSVersion(),
		"X-Client-Language":    "en-US",
		"X-Client-Timezone":    zcodeTimezone(),
		"X-Release-Channel":    "production",
		"X-Session-Id":         sessionID,
		"X-Query-Id":           queryID,
		"X-Request-Id":         requestID,
		"X-ZCode-Trace-Id":     traceID,
	}
	for name, value := range fingerprint {
		headers[name] = []string{value}
	}
	return nil
}

func newZCodeUUID() (string, error) {
	var id [16]byte
	if _, errRead := rand.Read(id[:]); errRead != nil {
		return "", errRead
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16]), nil
}

func zcodePlatform() string {
	goos := runtime.GOOS
	if goos == "windows" {
		goos = "win32"
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	return goos + "-" + arch
}

func zcodeOSCategory() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func detectZCodeOSVersion() string {
	command, args := "uname", []string{"-r"}
	if runtime.GOOS == "windows" {
		command, args = "cmd", []string{"/c", "ver"}
	}
	output, errOutput := exec.Command(command, args...).Output()
	version := strings.TrimSpace(string(output))
	if errOutput != nil || version == "" {
		return "unknown"
	}
	return version
}

func zcodeTimezone() string {
	zone := strings.TrimSpace(time.Local.String())
	if zone == "" || zone == "Local" {
		return "UTC"
	}
	return zone
}
