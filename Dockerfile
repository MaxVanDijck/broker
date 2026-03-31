FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -ldflags "-s -w" -o /broker-agent ./cmd/broker-agent
RUN CGO_ENABLED=1 go build -ldflags "-s -w" -o /broker-server ./cmd/broker-server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates docker-cli
COPY --from=builder /broker-agent /usr/local/bin/broker-agent
COPY --from=builder /broker-server /usr/local/bin/broker-server

ENTRYPOINT ["broker-agent"]
