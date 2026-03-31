package client

import (
	"net/http"

	"broker/proto/brokerpb/brokerpbconnect"
)

type Client struct {
	Broker brokerpbconnect.BrokerServiceClient
}

func New(baseURL string) *Client {
	return &Client{
		Broker: brokerpbconnect.NewBrokerServiceClient(
			http.DefaultClient,
			baseURL,
		),
	}
}
