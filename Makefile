GOBIN := $(shell go env GOPATH)/bin
VERSION ?= 0.1.0
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all build build-server build-cli build-agent dashboard proto test test-go test-helm fmt clean docs docs-serve update-pricing update-catalog

all: build

build: dashboard proto embed-agent build-cli build-server build-agent

build-cli:
	go build $(LDFLAGS) -o bin/broker ./cmd/broker

build-server: dashboard embed-agent
	go build $(LDFLAGS) -o bin/broker-server ./cmd/broker-server

build-agent:
	go build $(LDFLAGS) -o bin/broker-agent ./cmd/broker-agent

embed-agent:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o internal/server/agentbin/broker-agent ./cmd/broker-agent

dashboard:
	cd dashboard && bun run build
	rm -rf internal/dashboard/dist
	cp -r dashboard/dist internal/dashboard/dist

proto:
	PATH="$(GOBIN):$$PATH" protoc \
		--go_out=. --go_opt=module=broker \
		--connect-go_out=. --connect-go_opt=module=broker \
		proto/broker.proto
	PATH="$(GOBIN):$$PATH" protoc \
		--go_out=. --go_opt=module=broker \
		proto/agent.proto
	cd dashboard && npx buf generate ../proto

test: test-go test-helm

test-go:
	go vet ./...
	go test ./... -race -count=1

test-helm:
	helm lint charts/broker/ --strict
	./scripts/test-helm.sh

fmt:
	gofmt -s -w .

docs:
	cd docs && hugo --gc --minify --logLevel error

docs-serve:
	cd docs && hugo server --buildDrafts

update-pricing:
	go run scripts/update-pricing.go > internal/provider/aws/pricing_data.go
	gofmt -s -w internal/provider/aws/pricing_data.go

update-catalog:
	go run scripts/update-catalog.go > internal/provider/aws/catalog.go
	gofmt -s -w internal/provider/aws/catalog.go

clean:
	rm -rf bin/ docs/public/ dashboard/dist internal/dashboard/dist internal/server/agentbin/broker-agent
