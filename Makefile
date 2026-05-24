.PHONY: all deps build build-release test vet fmt clean run-root run-web install

VERSION ?= 0.1.0
LDFLAGS = -ldflags "-s -w -X github.com/alpine-webadmin/alpine-webadmin/pkg/version.Version=$(VERSION)"
BUILD_FLAGS = -trimpath $(LDFLAGS)

ALPINE_JS_URL = https://cdn.jsdelivr.net/npm/alpinejs@3.14.3/dist/cdn.min.js
ALPINE_JS_FILE = internal/frontend/assets/alpine.min.js

all: deps build

deps:
	@if [ ! -f $(ALPINE_JS_FILE) ] || [ $$(wc -c < $(ALPINE_JS_FILE)) -lt 1000 ]; then \
		curl -fsSL -o $(ALPINE_JS_FILE) $(ALPINE_JS_URL) || wget -q -O $(ALPINE_JS_FILE) $(ALPINE_JS_URL); \
	fi

build: deps
	CGO_ENABLED=0 go build -o bin/webadmin ./cmd/webadmin
	CGO_ENABLED=0 go build -o bin/roothelper ./cmd/roothelper

build-release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o bin/webadmin ./cmd/webadmin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o bin/roothelper ./cmd/roothelper

test:
	CGO_ENABLED=0 go test -v ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/

run-root: build
	./bin/roothelper -config etc/config.json -v

run-web: build
	./bin/webadmin -config etc/config.json -v

install:
	install -Dm755 bin/webadmin /usr/sbin/webadmin
	install -Dm755 bin/roothelper /usr/sbin/roothelper
	install -Dm640 etc/config.json /etc/webadmin/config.json
	install -Dm755 init/openrc/webadmin /etc/init.d/webadmin
	install -Dm755 init/openrc/roothelper /etc/init.d/roothelper
