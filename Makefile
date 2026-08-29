.PHONY: staticcheck unittest lint unittest build e2e

all: staticcheck errcheck lint unittest

staticcheck:
	go tool staticcheck ./...

errcheck:
	go tool errcheck ./...

lint:
	golangci-lint run ./...

unittest:
	go test ./...

# TODO(max): fix build and add to "all:" target
build:
	go build -gcflags=-m -o /dev/null .

# TODO(max): write an e2e test and add to "all:" target
e2e:
	docker compose down && docker compose up test --abort-on-container-exit --exit-code-from test
