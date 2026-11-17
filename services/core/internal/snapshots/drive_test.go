package snapshots

import (
	"bytes"
	"cloudforge/core/internal/databases"
	"cloudforge/core/internal/persistence"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryVault struct{ value string }

func (v *memoryVault) Get() (string, error)   { return v.value, nil }
func (v *memoryVault) Set(value string) error { v.value = value; return nil }
func (v *memoryVault) Delete() error          { v.value = ""; return nil }

func testService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Set("snapshotComputerModel", "Dell Inspiron 15 3520"); err != nil {
		t.Fatal(err)
	}
	s, err := New(t.Context(), store, databases.New(store, "test-installation-key"), "installation", dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.drive.state = driveState{Configured: true, Account: &account{ID: "google-account"}}
	s.drive.access = "test-access"
	s.drive.expires = time.Now().Add(time.Hour)
	return s
}

func insertSnapshotDB(t *testing.T, s *Service, id, engine string) {
	t.Helper()
	view, _ := json.Marshal(map[string]any{"View": databases.Instance{ID: id, Name: id, Engine: engine}})
	if _, err := s.store.DB.Exec("INSERT INTO hosted_databases(id,owner,engine,data) VALUES(?,?,?,?)", id, "owner", engine, string(view)); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotScheduleCivilTime(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := normalizeSchedule(Schedule{Frequency: "daily", Times: []string{"03:00", "02:30", "18:30"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if slot, ok := cfg.slot(time.Date(2026, 3, 8, 1, 0, 0, 0, loc), true); !ok || slot.Hour() != 3 || slot.Day() != 8 {
		t.Fatalf("spring forward: %v %v", slot, ok)
	}
	cfg.Times = []string{"01:30"}
	first, ok := cfg.slot(time.Date(2026, 11, 1, 0, 0, 0, 0, loc), true)
	_, offset := first.Zone()
	if !ok || offset != -4*3600 {
		t.Fatalf("first repeated slot: %v", first)
	}
	next, ok := cfg.slot(first.Add(time.Minute), true)
	if !ok || next.Day() != 2 {
		t.Fatalf("repeated slot was queued twice: %v", next)
	}
	cfg = Schedule{Frequency: "weekly", Days: []int{1, 3}, Times: []string{"06:00", "14:00", "22:00"}, Limit: 7}
	next, ok = cfg.slot(time.Date(2026, 11, 16, 6, 1, 0, 0, loc), true)
	if !ok || next.Weekday() != time.Monday || next.Hour() != 14 {
		t.Fatalf("second daily time: %v", next)
	}
	next, ok = cfg.slot(time.Date(2026, 11, 16, 22, 1, 0, 0, loc), true)
	if !ok || next.Weekday() != time.Wednesday || next.Hour() != 6 {
		t.Fatalf("next weekly day: %v", next)
	}
	if _, err := normalizeSchedule(Schedule{Frequency: "daily", Times: []string{"01:30", "01:30"}, Limit: 7}); err == nil {
		t.Fatal("duplicate times accepted")
	}
	if _, err := normalizeSchedule(Schedule{Frequency: "manual", Limit: 51}); err == nil {
		t.Fatal("limit above 50 accepted")
	}
}

func TestSnapshotSchedulePersistsAndRequiresConfirmation(t *testing.T) {
	s := testService(t)
	s.drive.state.Account = nil
	insertSnapshotDB(t, s, "db1", "postgresql")
	cfg := Schedule{Frequency: "weekly", Days: []int{1, 3}, Times: []string{"22:00", "06:00", "14:00"}, Limit: 12}
	if err := s.SaveSchedule("owner", "db1", cfg, false); err == nil {
		t.Fatal("lower retention did not require confirmation")
	}
	if err := s.SaveSchedule("owner", "db1", cfg, true); err != nil {
		t.Fatal(err)
	}
	id := int64(123456)
	if _, err := s.store.DB.Exec("UPDATE snapshot_settings SET last_slot=?", id); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAutomatic("owner", "db1", false); err != nil {
		t.Fatal(err)
	}
	s.Close()
	restarted, err := New(t.Context(), s.store, s.databases, s.installation, filepath.Dir(s.workDir))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	saved, err := restarted.schedule("db1", time.Now())
	if err != nil || saved.Limit != 12 || len(saved.Times) != 3 || saved.Times[0] != "06:00" ||
		saved.Frequency != "weekly" || len(saved.Days) != 2 || saved.Days[0] != 1 || saved.Days[1] != 3 ||
		saved.Last == nil || saved.Last.Unix() != id || !saved.Paused || saved.Next != nil {
		t.Fatalf("schedule after restart: %+v (%v)", saved, err)
	}
	beforeResume := time.Now()
	if err := restarted.SetAutomatic("owner", "db1", true); err != nil {
		t.Fatal(err)
	}
	resumed, err := restarted.schedule("db1", time.Now())
	if err != nil || resumed.Paused || resumed.Next == nil || resumed.Limit != saved.Limit ||
		resumed.Frequency != saved.Frequency || resumed.Last == nil || !resumed.Last.Equal(*saved.Last) ||
		!slices.Equal(resumed.Days, saved.Days) || !slices.Equal(resumed.Times, saved.Times) {
		t.Fatalf("resumed schedule: %+v (%v)", resumed, err)
	}
	var activeFrom int64
	if err := s.store.DB.QueryRow("SELECT active_from FROM snapshot_settings WHERE database_id='db1'").Scan(&activeFrom); err != nil || activeFrom < beforeResume.UnixNano() {
		t.Fatalf("schedule must skip slots before resuming: %d (%v)", activeFrom, err)
	}
	if _, err := restarted.StartRestore("owner", "db1", "snapshot", "", "", false); err == nil {
		t.Fatal("unconfirmed restore was accepted")
	}
}

func TestSnapshotStatusExpiresOperationNotices(t *testing.T) {
	s := testService(t)
	insertSnapshotDB(t, s, "db1", "postgresql")
	const key = "notice"
	for _, tc := range []struct {
		status  string
		age     time.Duration
		visible bool
	}{
		{"success", 0, true},
		{"success", time.Hour, false},
		{"warning", time.Hour, false},
		{"error", 0, true},
		{"error", time.Hour, false},
		{"running", time.Hour, true},
	} {
		raw, err := json.Marshal(Job{ID: key, Status: tc.status, Detail: "Test notice"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.store.DB.Exec("INSERT OR REPLACE INTO snapshot_jobs VALUES(?,?,?,?,?)", key, "db1", tc.status, string(raw), time.Now().Add(-tc.age).UnixNano())
		if err != nil {
			t.Fatal(err)
		}
		state, err := s.State(t.Context(), "owner", "db1", false)
		if err != nil || (state.Job != nil) != tc.visible || (state.Job != nil && state.Job.Status != tc.status) {
			t.Fatalf("status %s (%s): got %+v (%v)", tc.status, tc.age, state.Job, err)
		}
	}
}

func driveFixture(t *testing.T, s *Service) (func(driveFile), func(string) bool) {
	t.Helper()
	var mu sync.Mutex
	files := map[string]driveFile{}
	uploads := map[string][]byte{}
	var upload driveFile
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/upload/files":
			upload = driveFile{}
			if json.NewDecoder(r.Body).Decode(&upload) != nil {
				http.Error(w, "upload metadata", 400)
				return
			}
			w.Header().Set("Location", "http://"+r.Host+"/session")
			w.WriteHeader(200)
		case r.Method == "PUT" && r.URL.Path == "/session":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "upload data", 400)
				return
			}
			sum, _ := fingerprint(bytes.NewReader(data))
			upload.ID, upload.Created = upload.Props["snapshot"], time.Now().UTC()
			upload.Size, upload.MD5, upload.SHA256 = sum.Size, sum.MD5, sum.SHA256
			files[upload.ID], uploads[upload.ID] = upload, data
			_ = json.NewEncoder(w).Encode(upload)
		case r.Method == "GET" && r.URL.Path == "/files":
			items := []driveFile{}
			for _, f := range files {
				items = append(items, f)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"files": items})
		case r.Method == "POST" && r.URL.Path == "/files":
			var f driveFile
			if json.NewDecoder(r.Body).Decode(&f) != nil {
				http.Error(w, "metadata", 400)
				return
			}
			f.ID = fmt.Sprintf("folder-%d", len(files)+1)
			f.Created = time.Now().UTC()
			files[f.ID] = f
			_ = json.NewEncoder(w).Encode(f)
		case strings.HasPrefix(r.URL.Path, "/files/"):
			id := strings.TrimPrefix(r.URL.Path, "/files/")
			f, ok := files[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if r.Method == "DELETE" {
				delete(files, id)
				delete(uploads, id)
				w.WriteHeader(204)
				return
			}
			if r.URL.Query().Get("alt") == "media" {
				_, _ = w.Write(uploads[id])
				return
			}
			_ = json.NewEncoder(w).Encode(f)
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s.drive.client, s.drive.api, s.drive.uploadAPI = server.Client(), server.URL, server.URL+"/upload"
	return func(f driveFile) { mu.Lock(); defer mu.Unlock(); files[f.ID] = f },
		func(id string) bool { mu.Lock(); defer mu.Unlock(); _, ok := files[id]; return ok }
}

func TestDriveFoldersRetentionAndRecovery(t *testing.T) {
	s := testService(t)
	put, exists := driveFixture(t, s)
	insertSnapshotDB(t, s, "db1", "postgresql")
	insertSnapshotDB(t, s, "db2", "postgresql")
	a := databases.Instance{ID: "db1", Name: "NOME HOSPEDADO", Engine: "postgresql"}
	b := databases.Instance{ID: "db2", Engine: "postgresql"}
	ctx := t.Context()
	folder, err := s.folder(ctx, "google-account", a, true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.folder(ctx, "google-account", b, true)
	if err != nil {
		t.Fatal(err)
	}
	root, ok, err := s.drive.get(ctx, "google-account", folder.Parents[0])
	if err != nil || !ok || root.Name != "cloudforge - Dell Inspiron 15 3520" ||
		folder.Name != "PostgreSQL - db1" || other.Name != "PostgreSQL - db2" || other.Parents[0] != root.ID {
		t.Fatalf("folders: %+v %+v %+v (%v)", root, folder, other, err)
	}
	again, err := s.folder(ctx, "google-account", a, true)
	if err != nil || again.ID != folder.ID {
		t.Fatalf("duplicate folder: %v", err)
	}
	var n int
	if err = s.store.DB.QueryRow("SELECT count(*) FROM snapshot_drive_folders").Scan(&n); err != nil || n != 3 {
		t.Fatalf("persisted folder IDs: %d %v", n, err)
	}
	sum, _ := fingerprint(strings.NewReader("backup test"))
	makeItem := func(id string, parent driveFile, when time.Time) driveFile {
		mode := "manual"
		if when.Minute()%2 == 0 {
			mode = "automatic"
		}
		return driveFile{ID: id, Name: "snapshot_2026-11-16_06-00.dump", Parents: []string{parent.ID},
			Created: when, Size: sum.Size, MD5: sum.MD5, SHA256: sum.SHA256, Props: map[string]string{
				"cfKind": "snapshot", "installation": parent.Props["installation"], "database": parent.Props["database"],
				"engine": "postgresql", "mode": mode, "snapshot": id, "format": "dump", "version": "17.9", "md5": sum.MD5, "sha256": sum.SHA256}}
	}
	ref := time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)
	for i := range 51 {
		put(makeItem("file-"+strconv.Itoa(i), folder, ref.Add(time.Duration(i)*time.Minute)))
	}
	otherFile := makeItem("other-file", other, ref)
	put(otherFile)
	foreign := makeItem("foreign-file", folder, ref)
	foreign.Props["installation"] = "other-installation"
	put(foreign)
	unknown := makeItem("unknown-file", folder, ref)
	unknown.Props = nil
	put(unknown)
	invalid := makeItem("bad-checksum", folder, ref)
	invalid.MD5 = "invalid"
	put(invalid)
	newFile := makeItem("new-file", folder, ref.Add(52*time.Minute))
	// The upload is verified, but the listing can briefly lag.
	if err := s.retain(ctx, "google-account", folder, 50, newFile); err != nil {
		t.Fatal(err)
	}
	put(newFile)
	for _, id := range []string{"file-0", "file-1"} {
		if exists(id) {
			t.Fatalf("old snapshot %s was kept", id)
		}
	}
	for _, id := range []string{"other-file", "foreign-file", "unknown-file", "bad-checksum", "file-2", "new-file"} {
		if !exists(id) {
			t.Fatalf("wrong file %s was removed", id)
		}
	}
	state, err := s.State(ctx, "owner", "db1", true)
	if err != nil || state.ListError != "" || len(state.Snapshots) != 50 || state.Snapshots[0].DriveID != newFile.ID {
		t.Fatalf("timeline returned %d snapshots, %q (%v)", len(state.Snapshots), state.ListError, err)
	}
	if err := s.store.DB.QueryRow("SELECT count(*) FROM snapshot_history WHERE origin_folder=?", folder.ID).Scan(&n); err != nil || n != 50 {
		t.Fatalf("reconciled history: %d %v", n, err)
	}
	if _, err := s.store.DB.Exec("DELETE FROM hosted_databases WHERE id='db2'"); err != nil {
		t.Fatal(err)
	}
	available, err := s.Recoverable(ctx, "owner", "db1")
	if err != nil || len(available) != 1 || available[0].DriveID != "other-file" {
		t.Fatalf("deleted source recovery: %+v %v", available, err)
	}
	if _, err := s.remoteItem(ctx, "google-account", "other-file", "mysql"); err == nil {
		t.Fatal("cross-engine restoration accepted")
	}
	if _, err := s.remoteItem(ctx, "google-account", "bad-checksum", "postgresql"); err == nil {
		t.Fatal("invalid remote checksum accepted")
	}
	if err := s.SaveSchedule("owner", "db1", Schedule{Frequency: "manual", Limit: 3}, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool {
		job, _ := s.jobStatus("db1")
		return job != nil && job.Status == "success"
	})
	state, err = s.State(ctx, "owner", "db1", true)
	if err != nil || state.ListError != "" || len(state.Snapshots) != 3 || !exists("other-file") {
		t.Fatalf("retention after confirmed reduction: %d, %q (%v)", len(state.Snapshots), state.ListError, err)
	}
}

func TestDriveUploadResumesAfterLostResponse(t *testing.T) {
	s := testService(t)
	file, err := os.CreateTemp(t.TempDir(), "upload")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	payload := bytes.Repeat([]byte("snapshot"), 2*1024*1024)
	if _, err = file.Write(payload); err != nil {
		t.Fatal(err)
	}
	var received []byte
	var probes int
	sum, _ := fingerprint(bytes.NewReader(payload))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/files":
			w.Header().Set("Location", "http://"+r.Host+"/session")
			w.WriteHeader(200)
		case "/session":
			cr := r.Header.Get("Content-Range")
			if strings.HasPrefix(cr, "bytes */") {
				probes++
				w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(received)-1))
				w.WriteHeader(308)
				return
			}
			var start int
			if _, err := fmt.Sscanf(cr, "bytes %d-", &start); err != nil || start != len(received) {
				http.Error(w, "bad offset", 400)
				return
			}
			chunk, _ := io.ReadAll(r.Body)
			received = append(received, chunk...)
			if start == 0 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
				return
			}
			if len(received) < len(payload) {
				w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(received)-1))
				w.WriteHeader(308)
				return
			}
			_ = json.NewEncoder(w).Encode(driveFile{ID: "stored", Size: sum.Size, MD5: sum.MD5, SHA256: sum.SHA256})
		case "/files/stored":
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s.drive.client, s.drive.uploadAPI, s.drive.api = server.Client(), server.URL, server.URL
	progress := 0
	item, err := s.drive.upload(t.Context(), "google-account", file, int64(len(payload)), nil, func(v int) { progress = v })
	if err != nil || item.ID != "stored" || progress != 100 || probes != 1 || !bytes.Equal(received, payload) {
		t.Fatalf("resume: %+v %v (progress %d, probes %d)", item, err, progress, probes)
	}
	item.Props = map[string]string{"sha256": sum.SHA256, "md5": sum.MD5}
	var downloaded bytes.Buffer
	if err := s.drive.download(t.Context(), "google-account", item, &downloaded, func(int) {}); err != nil || !bytes.Equal(downloaded.Bytes(), payload) {
		t.Fatalf("download integrity: %v", err)
	}
	item.Props = maps.Clone(item.Props)
	item.Props["sha256"] = strings.Repeat("0", 64)
	if err := s.drive.download(t.Context(), "google-account", item, io.Discard, func(int) {}); err == nil {
		t.Fatal("corrupt download accepted")
	}
}

func TestJobConcurrencyAndPersistence(t *testing.T) {
	s := testService(t)
	insertSnapshotDB(t, s, "db1", "postgresql")
	block := make(chan struct{})
	_, err := s.start("owner", "db1", "testing", nil, func(j *job) error {
		select {
		case <-block:
		case <-j.ctx.Done():
			return j.ctx.Err()
		}
		j.stage("ready")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.start("owner", "db1", "conflict", nil, func(*job) error { return nil }); err != databases.ErrConflict {
		t.Fatalf("conflicting operation: %v", err)
	}
	close(block)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, err := s.jobStatus("db1")
		if err != nil {
			t.Fatal(err)
		}
		if j != nil && j.Status == "success" && j.Detail == "ready" {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("job did not finish")
}
