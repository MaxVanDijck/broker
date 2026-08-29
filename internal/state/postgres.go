package state

import (
	entity "broker/internal/state/entity"
	"uuid"
)

type postgresStore struct {
	// TODO(max): conn, etc
}

func NewPostgresStore() Store {
	return postgresStore{}
}


func (s postgresStore) CreateNode(node entity.Node) error {
	// TODO(max): implement
    return nil
}

func (s postgresStore) GetNode(id uuid.UUID) (*entity.Node, error) {
	// TODO(max): implement
	var node *entity.Node
    return node, nil
}

func (s postgresStore) ListNodes() ([]*entity.Node, error) {
	// TODO(max): implement
    return make([]*entity.Node, 0), nil
}

func (s postgresStore) UpdateNode(node entity.Node) error {
	// TODO(max): implement
    return nil
}
func (s postgresStore) DeleteNodeById(id uuid.UUID) error {
	// TODO(max): implement
    return nil
}

func (s postgresStore) Close() error {
	// TODO(max): implement
    return nil
}
