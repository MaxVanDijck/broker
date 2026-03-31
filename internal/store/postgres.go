package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"broker/internal/domain"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgres(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	s := &PostgresStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS clusters (
			name TEXT PRIMARY KEY,
			data JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			cluster_name TEXT NOT NULL,
			data JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_jobs_cluster ON jobs(cluster_name);
	`)
	return err
}

func (s *PostgresStore) CreateCluster(c *domain.Cluster) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO clusters (name, data) VALUES ($1, $2)", c.Name, data)
	return err
}

func (s *PostgresStore) GetCluster(name string) (*domain.Cluster, error) {
	var data []byte
	err := s.db.QueryRow("SELECT data FROM clusters WHERE name = $1", name).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c domain.Cluster
	return &c, json.Unmarshal(data, &c)
}

func (s *PostgresStore) ListClusters() ([]*domain.Cluster, error) {
	rows, err := s.db.Query("SELECT data FROM clusters ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []*domain.Cluster
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var c domain.Cluster
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
		clusters = append(clusters, &c)
	}
	return clusters, rows.Err()
}

func (s *PostgresStore) UpdateCluster(c *domain.Cluster) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE clusters SET data = $1 WHERE name = $2", data, c.Name)
	return err
}

func (s *PostgresStore) DeleteCluster(name string) error {
	_, err := s.db.Exec("DELETE FROM clusters WHERE name = $1", name)
	return err
}

func (s *PostgresStore) CreateJob(j *domain.Job) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO jobs (id, cluster_name, data) VALUES ($1, $2, $3)",
		j.ID, j.ClusterName, data)
	return err
}

func (s *PostgresStore) GetJob(id string) (*domain.Job, error) {
	var data []byte
	err := s.db.QueryRow("SELECT data FROM jobs WHERE id = $1", id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j domain.Job
	return &j, json.Unmarshal(data, &j)
}

func (s *PostgresStore) ListJobs(clusterName string) ([]*domain.Job, error) {
	rows, err := s.db.Query("SELECT data FROM jobs WHERE cluster_name = $1 ORDER BY created_at DESC",
		clusterName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var j domain.Job
		if err := json.Unmarshal(data, &j); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}

func (s *PostgresStore) ListAllJobs() ([]*domain.Job, error) {
	rows, err := s.db.Query("SELECT data FROM jobs ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var j domain.Job
		if err := json.Unmarshal(data, &j); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}

func (s *PostgresStore) UpdateJob(j *domain.Job) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE jobs SET data = $1 WHERE id = $2", data, j.ID)
	return err
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// compile-time checks
var _ StateStore = (*PostgresStore)(nil)
var _ StateStore = (*SQLiteStore)(nil)

// suppress unused import
var _ = time.Now
