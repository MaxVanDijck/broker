package aws

import (
	"log/slog"

	"broker/internal/provider"
)

func Register(registry *provider.Registry, logger *slog.Logger, serverURL string) {
	p := New(logger.With("provider", "aws"), serverURL)
	registry.Register(p)
}
