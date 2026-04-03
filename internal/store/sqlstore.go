package store

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"broker/internal/domain"
)

// SQLStore is the single StateStore implementation backed by database/sql.
// It works with any SQL driver (SQLite, PostgreSQL) -- dialect differences
// are handled by rebinding placeholders.
type SQLStore struct {
	db     *sql.DB
	driver string // "sqlite3" or "pgx"
}

func (s *SQLStore) rebind(query string) string {
	if s.driver != "pgx" {
		return query
	}
	var b strings.Builder
	n := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			n++
		} else {
			b.WriteByte(query[i])
		}
	}
	return b.String()
}

func (s *SQLStore) CreateCluster(c *domain.Cluster) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		s.rebind("INSERT INTO clusters (id, name, data, created_at) VALUES (?, ?, ?, ?)"),
		c.ID, c.Name, data, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLStore) GetCluster(name string) (*domain.Cluster, error) {
	rows, err := s.db.Query(
		s.rebind("SELECT data FROM clusters WHERE name = ? ORDER BY created_at DESC"),
		name,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var c domain.Cluster
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
		if c.Status != domain.ClusterStatusTerminated && c.Status != domain.ClusterStatusTerminating {
			return &c, nil
		}
	}
	return nil, rows.Err()
}

func (s *SQLStore) GetClusterByID(id string) (*domain.Cluster, error) {
	var data []byte
	err := s.db.QueryRow(
		s.rebind("SELECT data FROM clusters WHERE id = ?"),
		id,
	).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c domain.Cluster
	return &c, json.Unmarshal(data, &c)
}

func (s *SQLStore) ListClusters() ([]*domain.Cluster, error) {
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

func (s *SQLStore) UpdateCluster(c *domain.Cluster) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		s.rebind("UPDATE clusters SET name = ?, data = ? WHERE id = ?"),
		c.Name, data, c.ID,
	)
	return err
}

func (s *SQLStore) CreateJob(j *domain.Job) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		s.rebind("INSERT INTO jobs (id, cluster_id, data, created_at) VALUES (?, ?, ?, ?)"),
		j.ID, j.ClusterID, data, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLStore) GetJob(id string) (*domain.Job, error) {
	var data []byte
	err := s.db.QueryRow(
		s.rebind("SELECT data FROM jobs WHERE id = ?"),
		id,
	).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j domain.Job
	return &j, json.Unmarshal(data, &j)
}

func (s *SQLStore) ListJobs(clusterID string) ([]*domain.Job, error) {
	rows, err := s.db.Query(
		s.rebind("SELECT data FROM jobs WHERE cluster_id = ? ORDER BY created_at DESC"),
		clusterID,
	)
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

func (s *SQLStore) ListAllJobs() ([]*domain.Job, error) {
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

func (s *SQLStore) UpdateJob(j *domain.Job) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		s.rebind("UPDATE jobs SET data = ? WHERE id = ?"),
		data, j.ID,
	)
	return err
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

var _ StateStore = (*SQLStore)(nil)
