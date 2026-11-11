package persistence

import (
	"cloudforge/core/internal/model"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ DB *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db}
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY)"} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	files, _ := migrations.ReadDir("migrations")
	for _, f := range files {
		var n int
		if err = db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=?", f.Name()).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			continue
		}
		data, _ := migrations.ReadFile("migrations/" + f.Name())
		tx, e := db.Begin()
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(string(data)); e == nil {
			_, e = tx.Exec("INSERT INTO schema_migrations VALUES(?)", f.Name())
		}
		if e != nil {
			tx.Rollback()
			db.Close()
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Setting(key string, out any) error {
	var raw string
	err := s.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&raw)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), out)
}
func (s *Store) Set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at", key, string(raw), time.Now().UTC())
	return err
}
func (s *Store) Projects() ([]model.Project, error) {
	rows, e := s.DB.Query("SELECT data FROM projects ORDER BY created_at DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		var raw string
		var p model.Project
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) Project(id string) (model.Project, error) {
	var p model.Project
	var raw string
	e := s.DB.QueryRow("SELECT data FROM projects WHERE id=?", id).Scan(&raw)
	if e != nil {
		return p, e
	}
	e = json.Unmarshal([]byte(raw), &p)
	return p, e
}
func (s *Store) SaveProject(p model.Project) error {
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO projects(id,name,path,runtime,desired_state,data,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,path=excluded.path,runtime=excluded.runtime,desired_state=excluded.desired_state,data=excluded.data,updated_at=excluded.updated_at", p.ID, p.Name, p.Path, p.Runtime, p.DesiredState, string(raw), p.CreatedAt, p.UpdatedAt)
	return e
}
func (s *Store) DeleteProject(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		"DELETE FROM project_routes WHERE project_id=?",
		"DELETE FROM project_runtime WHERE project_id=?",
		"DELETE FROM project_dependencies WHERE project_id=?",
		"DELETE FROM operations WHERE json_extract(payload,'$.projectId')=?1 OR json_extract(payload,'$.route.projectId')=?1",
		`UPDATE operations SET payload=json_set(payload,'$.previousRoutes',json((
			SELECT json_group_array(json(value)) FROM json_each(payload,'$.previousRoutes')
			WHERE json_extract(value,'$.projectId') IS NOT ?1
		))) WHERE EXISTS (SELECT 1 FROM json_each(payload,'$.previousRoutes') WHERE json_extract(value,'$.projectId')=?1)`,
		"DELETE FROM projects WHERE id=?",
	} {
		if _, err = tx.Exec(statement, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Desired(id, state string) error {
	p, e := s.Project(id)
	if e != nil {
		return e
	}
	p.DesiredState = state
	p.UpdatedAt = time.Now().UTC()
	return s.SaveProject(p)
}
func (s *Store) SaveProjectPort(p model.Project) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRow("SELECT data FROM project_routes WHERE project_id=?", p.ID).Scan(&raw)
	if err == nil {
		if p.Port == 0 {
			return errors.New("remova o endereço público antes de deixar o projeto sem porta")
		}
		var route model.Route
		if err = json.Unmarshal(raw, &route); err != nil {
			return err
		}
		route.Port = p.Port
		raw, err = json.Marshal(route)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE project_routes SET local_port=?,data=? WHERE project_id=?", p.Port, string(raw), p.ID); err != nil {
			return err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	raw, err = json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE projects SET data=?,updated_at=? WHERE id=?", string(raw), p.UpdatedAt, p.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Runtime(r model.Runtime) error {
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO project_runtime(project_id,pid,actual_state,data) VALUES(?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET pid=excluded.pid,actual_state=excluded.actual_state,data=excluded.data", r.ProjectID, r.PID, r.ActualState, string(raw))
	return e
}
func (s *Store) PreviousRuntime(id string) (model.Runtime, error) {
	var r model.Runtime
	var raw string
	e := s.DB.QueryRow("SELECT data FROM project_runtime WHERE project_id=?", id).Scan(&raw)
	if e != nil {
		return r, e
	}
	e = json.Unmarshal([]byte(raw), &r)
	return r, e
}
func (s *Store) Dependencies(id string) (model.Dependencies, error) {
	var d model.Dependencies
	var raw string
	e := s.DB.QueryRow("SELECT data FROM project_dependencies WHERE project_id=?", id).Scan(&raw)
	if e != nil {
		return d, e
	}
	e = json.Unmarshal([]byte(raw), &d)
	return d, e
}
func (s *Store) SaveDependencies(d model.Dependencies) error {
	raw, e := json.Marshal(d)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO project_dependencies(project_id,data) VALUES(?,?) ON CONFLICT(project_id) DO UPDATE SET data=excluded.data", d.ProjectID, string(raw))
	return e
}
func (s *Store) Routes() ([]model.Route, error) {
	rows, e := s.DB.Query("SELECT data FROM project_routes")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Route{}
	for rows.Next() {
		var raw string
		var r model.Route
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &r); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) SaveRoute(r model.Route) error {
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO project_routes(id,project_id,hostname,local_port,data) VALUES(?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET hostname=excluded.hostname,local_port=excluded.local_port,data=excluded.data", r.ID, r.ProjectID, r.Hostname, r.Port, string(raw))
	return e
}
func (s *Store) DeleteRoute(projectID string) error {
	_, err := s.DB.Exec("DELETE FROM project_routes WHERE project_id=?", projectID)
	return err
}
func (s *Store) SaveCloudConfig(cfg model.CloudConfig) error {
	raw, e := json.Marshal(cfg)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO settings(key,value,updated_at) VALUES('cloudflare',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at", []any{string(raw), time.Now().UTC()}},
		{"INSERT OR REPLACE INTO domains VALUES(?,?,?,?)", []any{cfg.ZoneID, cfg.ZoneID, cfg.Domain, "cloudflare"}},
		{"INSERT OR REPLACE INTO tunnels VALUES(?,?,?,?,?)", []any{cfg.TunnelID, cfg.TunnelID, cfg.Name, "configured", "os-keyring:tunnel-token"}},
	} {
		if _, e = tx.Exec(statement.sql, statement.args...); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Operation(id, kind, status string, payload any, lastError string) error {
	raw, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO operations(id,kind,status,payload,last_error,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,payload=excluded.payload,last_error=excluded.last_error,updated_at=excluded.updated_at", id, kind, status, string(raw), lastError, time.Now().UTC())
	return e
}
func (s *Store) PendingOperations() ([]map[string]any, error) {
	rows, e := s.DB.Query("SELECT id,kind,status,payload,last_error FROM operations WHERE status NOT IN ('complete','rolled_back','failed','cancelled')")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, kind, status, payload, last string
		if e = rows.Scan(&id, &kind, &status, &payload, &last); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"id": id, "kind": kind, "status": status, "payload": json.RawMessage(payload), "lastError": last})
	}
	return out, rows.Err()
}

var ErrConflict = errors.New("registro já existe")

func Wrap(err error, what string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}
