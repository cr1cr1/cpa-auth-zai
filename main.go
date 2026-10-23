package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const abiVersion uint32 = 1

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	installHostCaller(cgoHostCall)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errEnvelope("invalid_method", "method is required", 0))
		return 1
	}
	req := []byte("{}")
	if request != nil && requestLen > 0 {
		req = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleCall(C.GoString(method), req)
	if errHandle != nil {
		status := 0
		type statusCoder interface{ StatusCode() int }
		var sc statusCoder
		if errors.As(errHandle, &sc) && sc != nil {
			status = sc.StatusCode()
		}
		writeResponse(response, errEnvelope("plugin_error", errHandle.Error(), status))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	shutdown()
}

// cgoHostCall issues one host callback RPC across the C ABI.
func cgoHostCall(method string, payload []byte) ([]byte, error) {
	var req []byte
	if len(payload) > 0 {
		req = payload
	} else {
		req = []byte("{}")
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var resp C.cliproxy_buffer
	code := C.call_host_api(cMethod, (*C.uint8_t)(unsafe.Pointer(&req[0])), C.size_t(len(req)), &resp)
	if code != 0 {
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	if resp.ptr == nil || resp.len == 0 {
		return nil, fmt.Errorf("host callback %s returned an empty response", method)
	}
	out := C.GoBytes(unsafe.Pointer(resp.ptr), C.int(resp.len))
	C.free_host_buffer(resp.ptr, resp.len)
	return out, nil
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil {
		return
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	buf := (*C.uint8_t)(C.CBytes(raw))
	response.ptr = unsafe.Pointer(buf)
	response.len = C.size_t(len(raw))
}
