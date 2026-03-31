package provider

import (
	"context"

	"broker/internal/domain"
)

type NodeInfo struct {
	InstanceID string
	PublicIP   string
	PrivateIP  string
	Status     string
}

type Provider interface {
	Name() domain.CloudProvider

	Launch(ctx context.Context, cluster *domain.Cluster, task *domain.TaskSpec) ([]NodeInfo, error)
	Stop(ctx context.Context, cluster *domain.Cluster) error
	Start(ctx context.Context, cluster *domain.Cluster) error
	Teardown(ctx context.Context, cluster *domain.Cluster) error
	Status(ctx context.Context, cluster *domain.Cluster) (domain.ClusterStatus, error)
}

type Registry struct {
	providers map[domain.CloudProvider]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[domain.CloudProvider]Provider)}
}

func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
}

func (r *Registry) Get(cloud domain.CloudProvider) (Provider, bool) {
	p, ok := r.providers[cloud]
	return p, ok
}

func (r *Registry) List() []domain.CloudProvider {
	clouds := make([]domain.CloudProvider, 0, len(r.providers))
	for c := range r.providers {
		clouds = append(clouds, c)
	}
	return clouds
}
