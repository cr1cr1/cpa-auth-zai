PLUGIN_ID := cpa-auth-zai
VERSION := 0.1.0
DIST := dist
SO := $(DIST)/$(PLUGIN_ID).so
# Override for `make install`, e.g. the CPA plugins directory mounted by your deployment.
PLUGIN_DIR ?= /home/cri/.config/containers/cli-proxy-api/plugins/linux/amd64

export GOCACHE ?= /tmp/$(PLUGIN_ID)-gocache
export GOMODCACHE ?= /tmp/$(PLUGIN_ID)-gomod

.PHONY: test vet fmt build install clean

test: 
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

build: $(SO)

$(SO): $(wildcard *.go) go.mod
	CGO_ENABLED=1 go build -buildmode=c-shared -o $@ .
	@rm -f $(SO).h

install: $(SO)
	install -m 0755 $(SO) $(PLUGIN_DIR)/$(PLUGIN_ID)-v$(VERSION).so

clean:
	rm -rf $(DIST)
