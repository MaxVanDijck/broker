package state

import (
	entity "broker/internal/state/entity"
	"uuid"
)

// StateStore handles nodes, jobs, users.
type Store interface {
	// TODO(max) pass context to methods
	CreateNode(node entity.Node) error
	GetNode(id uuid.UUID) (*entity.Node, error)
	ListNodes() ([]*entity.Node, error)
	UpdateNode(node entity.Node) error
	DeleteNodeById(id uuid.UUID) error

	Close() error
}

