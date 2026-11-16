package snapshots

import (
	"cloudforge/core/internal/databases"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduledAndManualSnapshotsDocker(t *testing.T) {
	if os.Getenv("CLOUDFORGE_SNAPSHOT_DOCKER_TEST") != "1" {
		t.Skip("requires local Docker and the Cloudforge PostgreSQL image")
	}
	s := testService(t)
	driveFixture(t, s)
	m := s.databases
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx) }()
	const owner = "snapshot-test"
	const name = "Snapshot scheduler test"
	var id string
	defer func() {
		defer func() { cancel(); <-done }()
		if id != "" {
			waitFor(t, 30*time.Second, func() bool {
				_, err := m.Change(owner, id, databases.Change{Action: "delete", Confirmation: name})
				return err == nil || errors.Is(err, databases.ErrNotFound)
			})
			waitFor(t, time.Minute, func() bool {
				_, err := m.SnapshotDatabase(owner, id)
				return errors.Is(err, databases.ErrNotFound)
			})
		}
	}()
	waitFor(t, time.Minute, func() bool {
		view, err := m.List(ctx, owner)
		return err == nil && view.Docker
	})
	db, err := m.Create(ctx, owner, databases.Input{Name: name, Engine: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	id = db.ID
	waitFor(t, 90*time.Second, func() bool { return m.SnapshotReady(owner, id) })
	now := s.localNow()
	due := now.Add(-time.Minute)
	next := now.Add(20 * time.Minute)
	cfg := Schedule{Frequency: "daily", Times: []string{due.Format("15:04"), next.Format("15:04")}, Limit: 50}
	if err := s.SaveSchedule(owner, id, cfg, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.DB.Exec("UPDATE snapshot_settings SET active_from=? WHERE database_id=?", due.Add(-time.Minute).UnixNano(), id); err != nil {
		t.Fatal(err)
	}
	if err := s.scheduled(now); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 90*time.Second, func() bool { j, _ := s.jobStatus(id); return j != nil && j.Status != "running" })
	automatic, _ := s.jobStatus(id)
	if automatic.Status != "success" {
		t.Fatalf("scheduled snapshot: %+v", automatic)
	}
	if err := s.scheduled(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if j, err := s.jobStatus(id); err != nil || j.ID != automatic.ID {
		t.Fatalf("repeated scheduling slot: %+v %v", j, err)
	}

	j, err := s.StartBackup(owner, id)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, s, id, j.ID)
	state, err := s.State(t.Context(), owner, id, true)
	if err != nil || state.ListError != "" || len(state.Snapshots) != 2 || state.Snapshots[0].Mode != "manual" || state.Snapshots[1].Mode != "automatic" {
		t.Fatalf("timeline: %+v (%v)", state, err)
	}
	path := filepath.Join(t.TempDir(), state.Snapshots[0].Name)
	j, err = s.StartDownload(owner, id, state.Snapshots[0].DriveID, path)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, s, id, j.ID)
	j, err = s.StartRestore(owner, id, "", path, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, s, id, j.ID)
}

func waitForJob(t *testing.T, s *Service, id, jobID string) {
	t.Helper()
	waitFor(t, 90*time.Second, func() bool {
		j, err := s.jobStatus(id)
		if err != nil || j == nil || j.ID != jobID || j.Status == "running" {
			return false
		}
		if j.Status != "success" {
			t.Fatalf("snapshot operation: %+v", j)
		}
		return true
	})
}

func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("snapshot condition did not complete within the deadline")
}
