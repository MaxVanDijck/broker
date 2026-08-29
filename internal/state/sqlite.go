package state

import (
    entity "broker/internal/state/entity"
    "uuid"
)

type sqliteStore struct {
    // TODO(max): conn, etc
}

func NewSqliteStore() Store {
    return sqliteStore{}
}


func (s sqliteStore) CreateNode(node entity.Node) error {
    // TODO(max): implement
    return nil
}

func (s sqliteStore) GetNode(id uuid.UUID) (*entity.Node, error) {
    // TODO(max): implement
    var node *entity.Node
    return node, nil
}

func (s sqliteStore) ListNodes() ([]*entity.Node, error) {
    // TODO(max): implement
    return make([]*entity.Node, 0), nil
}

func (s sqliteStore) UpdateNode(node entity.Node) error {
    // TODO(max): implement
    return nil
}
func (s sqliteStore) DeleteNodeById(id uuid.UUID) error {
    // TODO(max): implement
    return nil
}

func (s sqliteStore) Close() error {
    // TODO(max): implement
    return nil
}
