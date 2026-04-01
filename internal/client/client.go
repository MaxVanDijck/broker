package client

import (
	"encoding/base64"
	"net/http"

	"broker/proto/brokerpb/brokerpbconnect"
)

type Client struct {
	Broker brokerpbconnect.BrokerServiceClient
}

func New(baseURL string, token string) *Client {
	httpClient := http.DefaultClient
	if token != "" {
		httpClient = &http.Client{
			Transport: &authTransport{
				base:  http.DefaultTransport,
				token: token,
			},
		}
	}

	return &Client{
		Broker: brokerpbconnect.NewBrokerServiceClient(
			httpClient,
			baseURL,
		),
	}
}

type authTransport struct {
	base  http.RoundTripper
	token string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("broker:"+t.token)))
	return t.base.RoundTrip(r)
}
