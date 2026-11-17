package snapshots

import (
	"cloudforge/core/internal/databases"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/processes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

type Schedule struct {
	Frequency string     `json:"frequency"`
	Days      []int      `json:"days"`
	Times     []string   `json:"times"`
	Limit     int        `json:"limit"`
	Paused    bool       `json:"paused"`
	Last      *time.Time `json:"last,omitempty"`
	Next      *time.Time `json:"next,omitempty"`
}

func defaultSchedule() Schedule {
	return Schedule{Frequency: "manual", Days: []int{}, Times: []string{}, Limit: 50}
}

func normalizeSchedule(v Schedule) (Schedule, error) {
	if v.Limit < 1 || v.Limit > 50 {
		return v, errors.New("informe um limite de 1 a 50 snapshots")
	}
	v.Last, v.Next, v.Paused = nil, nil, false
	switch v.Frequency {
	case "manual":
		v.Days, v.Times = []int{}, []string{}
		return v, nil
	case "weekly":
		if len(v.Days) < 1 || len(v.Days) > 7 {
			return v, errors.New("selecione os dias da semana")
		}
		slices.Sort(v.Days)
		for i, day := range v.Days {
			if day < 0 || day > 6 || (i > 0 && day == v.Days[i-1]) {
				return v, errors.New("dias da semana inválidos")
			}
		}
	case "daily":
		v.Days = []int{}
	default:
		return v, errors.New("frequência inválida")
	}
	if len(v.Times) < 1 || len(v.Times) > 1440 {
		return v, errors.New("informe pelo menos um horário de execução")
	}
	slices.Sort(v.Times)
	for i, clock := range v.Times {
		t, err := time.Parse("15:04", clock)
		if err != nil || t.Format("15:04") != clock || (i > 0 && clock == v.Times[i-1]) {
			return v, errors.New("informe horários válidos sem duplicatas")
		}
	}
	return v, nil
}

func (s *Service) schedule(id string, now time.Time) (Schedule, error) {
	v := defaultSchedule()
	var raw string
	var last sql.NullInt64
	var paused bool
	err := s.store.DB.QueryRow("SELECT settings,last_slot,paused FROM snapshot_settings WHERE database_id=?", id).Scan(&raw, &last, &paused)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return v, err
	}
	v.Paused = paused
	if last.Valid {
		t := time.Unix(last.Int64, 0)
		v.Last = &t
	}
	if next, ok := v.slot(now, true); ok {
		v.Next = &next
	}
	return v, nil
}

// slot walks civil dates. A nonexistent clock time is skipped and a repeated one occurs once.
func (v Schedule) slot(now time.Time, future bool) (time.Time, bool) {
	if v.Paused || v.Frequency == "manual" || len(v.Times) == 0 {
		return time.Time{}, false
	}
	var closest time.Time
	dir := -1
	if future {
		dir = 1
	}
	for i := 0; i <= 8; i++ {
		date := now.AddDate(0, 0, i*dir)
		y, m, d := date.Date()
		if v.Frequency == "weekly" && !slices.Contains(v.Days, int(date.Weekday())) {
			continue
		}
		_, before := time.Date(y, m, d, 0, 0, 0, 0, date.Location()).Add(-12 * time.Hour).Zone()
		_, after := time.Date(y, m, d, 23, 59, 0, 0, date.Location()).Add(12 * time.Hour).Zone()
		for _, clock := range v.Times {
			c, err := time.Parse("15:04", clock)
			if err != nil {
				continue
			}
			wall := time.Date(y, m, d, c.Hour(), c.Minute(), 0, 0, time.UTC)
			var first time.Time
			for _, offset := range []int{before, after} {
				t := wall.Add(-time.Duration(offset) * time.Second).In(date.Location())
				if t.Year() == y && t.Month() == m && t.Day() == d && t.Hour() == c.Hour() && t.Minute() == c.Minute() &&
					(first.IsZero() || t.Before(first)) {
					first = t
				}
			}
			if first.IsZero() {
				continue
			}
			if future && first.After(now) && (closest.IsZero() || first.Before(closest)) {
				closest = first
			}
			if !future && !first.After(now) && (closest.IsZero() || first.After(closest)) {
				closest = first
			}
		}
		if !closest.IsZero() {
			return closest, true
		}
	}
	return time.Time{}, false
}

func (s *Service) scheduled(now time.Time) error {
	state := s.drive.status()
	if state.Account == nil || !state.Configured || state.Error == errRevoked.Error() {
		return nil
	}
	rows, err := s.store.DB.Query("SELECT h.id,h.owner,s.settings,s.active_from,s.last_slot FROM snapshot_settings s JOIN hosted_databases h ON h.id=s.database_id WHERE s.paused=0")
	if err != nil {
		return err
	}
	type due struct {
		id, owner    string
		slot, active int64
	}
	var work []due
	for rows.Next() {
		var id, owner, raw string
		var active int64
		var last sql.NullInt64
		if err := rows.Scan(&id, &owner, &raw, &active, &last); err != nil {
			rows.Close()
			return err
		}
		var cfg Schedule
		if json.Unmarshal([]byte(raw), &cfg) != nil {
			continue
		}
		slot, ok := cfg.slot(now, false)
		if ok && slot.UnixNano() >= active && (!last.Valid || slot.Unix() > last.Int64) {
			work = append(work, due{id, owner, slot.Unix(), active})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range work {
		if !s.databases.SnapshotReady(item.owner, item.id) {
			continue
		}
		_, err := s.start(item.owner, item.id, "Preparando snapshot automático", func(tx *sql.Tx) error {
			change, err := tx.Exec("UPDATE snapshot_settings SET last_slot=? WHERE database_id=? AND paused=0 AND active_from=? AND (last_slot IS NULL OR last_slot < ?)", item.slot, item.id, item.active, item.slot)
			if err != nil {
				return err
			}
			if n, err := change.RowsAffected(); err != nil || n != 1 {
				return databases.ErrConflict
			}
			return nil
		}, func(j *job) error { return s.backup(j, item.owner, item.id, "automatic", state.Account.ID) })
		if err != nil && !errors.Is(err, databases.ErrConflict) {
			return err
		}
	}
	return nil
}

func (s *Service) localNow() time.Time {
	// Loading the named zone on Unix follows system updates without relying on time.Local's cache.
	if runtime.GOOS != "windows" && os.Getenv("TZ") == "" {
		if zone, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
			if _, name, ok := strings.Cut(filepath.ToSlash(zone), "/zoneinfo/"); ok {
				if loc, err := time.LoadLocation(name); err == nil {
					return time.Now().In(loc)
				}
			}
		}
	}
	return time.Now()
}

func computerModel(ctx context.Context, store *persistence.Store) string {
	var saved string
	if store.Setting("snapshotComputerModel", &saved) == nil && saved != "" {
		return saved
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result string
	if runtime.GOOS == "windows" {
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding=[Text.Encoding]::UTF8; (Get-CimInstance Win32_ComputerSystem).Model")
		processes.ConfigureBackground(cmd)
		out, _ := cmd.Output()
		result = string(out)
	} else {
		out, _ := os.ReadFile("/sys/class/dmi/id/product_name")
		result = string(out)
		if runtime.GOOS == "darwin" {
			out, _ := exec.CommandContext(ctx, "sysctl", "-n", "hw.model").Output()
			result = string(out)
		}
	}
	result = strings.Join(strings.Fields(result), " ")
	switch strings.ToLower(result) {
	case "", "system product name", "default string", "to be filled by o.e.m.":
		result, _ = os.Hostname()
	}
	result = strings.NewReplacer("/", "-", "\\", "-").Replace(result)
	if result == "" {
		result = fmt.Sprintf("computador %s", runtime.GOOS)
	}
	if len([]rune(result)) > 90 {
		result = string([]rune(result)[:90])
	}
	_ = store.Set("snapshotComputerModel", result)
	return result
}
