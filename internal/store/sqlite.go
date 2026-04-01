package store

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"broker/internal/domain"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLite(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS clusters (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_clusters_name ON clusters(name);
		CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			cluster_id TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_jobs_cluster_id ON jobs(cluster_id);
	`)
	return err
}

func (s *SQLiteStore) CreateCluster(c *domain.Cluster) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO clusters (id, name, data, created_at) VALUES (?, ?, ?, ?)",
		c.ID, c.Name, string(data), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) GetCluster(name string) (*domain.Cluster, error) {
	var data string
	err := s.db.QueryRow("SELECT data FROM clusters WHERE name = ?", name).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c domain.Cluster
	return &c, json.Unmarshal([]byte(data), &c)
}

func (s *SQLiteStore) GetClusterByID(id string) (*domain.Cluster, error) {
	var data string
	err := s.db.QueryRow("SELECT data FROM clusters WHERE id = ?", id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c domain.Cluster
	return &c, json.Unmarshal([]byte(data), &c)
}

func (s *SQLiteStore) ListClusters() ([]*domain.Cluster, error) {
	rows, err := s.db.Query("SELECT data FROM clusters ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []*domain.Cluster
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var c domain.Cluster
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return nil, err
		}
		clusters = append(clusters, &c)
	}
	return clusters, rows.Err()
}

func (s *SQLiteStore) UpdateCluster(c *domain.Cluster) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE clusters SET name = ?, data = ? WHERE id = ?", c.Name, string(data), c.ID)
	return err
}

func (s *SQLiteStore) DeleteCluster(id string) error {
	_, err := s.db.Exec("DELETE FROM clusters WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) CreateJob(j *domain.Job) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO jobs (id, cluster_id, data, created_at) VALUES (?, ?, ?, ?)",
		j.ID, j.ClusterID, string(data), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) GetJob(id string) (*domain.Job, error) {
	var data string
	err := s.db.QueryRow("SELECT data FROM jobs WHERE id = ?", id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j domain.Job
	return &j, json.Unmarshal([]byte(data), &j)
}

func (s *SQLiteStore) ListJobs(clusterID string) ([]*domain.Job, error) {
	rows, err := s.db.Query("SELECT data FROM jobs WHERE cluster_id = ? ORDER BY created_at DESC", clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var j domain.Job
		if err := json.Unmarshal([]byte(data), &j); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}

func (s *SQLiteStore) ListAllJobs() ([]*domain.Job, error) {
	rows, err := s.db.Query("SELECT data FROM jobs ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var j domain.Job
		if err := json.Unmarshal([]byte(data), &j); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}

func (s *SQLiteStore) UpdateJob(j *domain.Job) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE jobs SET data = ? WHERE id = ?", string(data), j.ID)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
