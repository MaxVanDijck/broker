package store

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

func NewSQLite(path string) (*SQLStore, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	if err := migrateSQLite(db); err != nil {
		return nil, err
	}

	return &SQLStore{db: db, driver: "sqlite3"}, nil
}

func migrateSQLite(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS clusters (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_clusters_name ON clusters(name);
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
