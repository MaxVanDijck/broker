FROM oven/bun:1 AS dashboard
WORKDIR /dashboard
COPY dashboard/package.json dashboard/bun.lockb ./
RUN bun install --frozen-lockfile
COPY dashboard/ .
RUN bun run build

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev

ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=dashboard /dashboard/dist internal/dashboard/dist

# Build agent binary for linux/amd64 first, then embed it in the server.
# The agent is also copied to the final image as a standalone binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.version=${VERSION}" -o internal/server/agentbin/broker-agent ./cmd/broker-agent && \
    CGO_ENABLED=1 go build -ldflags "-s -w -X main.version=${VERSION}" -o /broker-server ./cmd/broker-server && \
    cp internal/server/agentbin/broker-agent /broker-agent

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /broker-agent /usr/local/bin/broker-agent
COPY --from=builder /broker-server /usr/local/bin/broker-server

ENTRYPOINT ["broker-server"]
