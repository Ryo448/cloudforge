package api

import (
	"cloudforge/core/internal/snapshots"
	"errors"
	"net/http"
)

func (s *Server) snapshotRoutes(w http.ResponseWriter, r *http.Request, owner string, parts []string) {
	w.Header().Set("Cache-Control", "no-store")
	if s.Snapshots == nil {
		fail(w, 503, errors.New("serviço de snapshots indisponível"))
		return
	}
	v := s.Snapshots
	if len(parts) == 3 && parts[1] == "snapshots" && parts[2] == "drive" {
		switch r.Method {
		case http.MethodGet:
			write(w, http.StatusOK, v.DriveStatus())
		case http.MethodPut:
			var client snapshots.OAuthClient
			err := decode(w, r, &client)
			if err == nil {
				err = v.ConfigureDrive(client)
			}
			if err != nil {
				databaseError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 4 && parts[1] == "snapshots" && parts[2] == "drive" && r.Method == "POST" {
		switch parts[3] {
		case "connect":
			url, err := v.Connect()
			if err != nil {
				databaseError(w, err)
				return
			}
			write(w, 202, map[string]string{"url": url})
		case "disconnect":
			if err := v.Disconnect(r.Context()); err != nil {
				databaseError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
		return
	}
	if len(parts) < 4 || len(parts) > 5 || parts[1] != "databases" || parts[3] != "snapshots" {
		http.NotFound(w, r)
		return
	}
	id := parts[2]
	action := ""
	if len(parts) == 5 {
		action = parts[4]
	}
	var value any
	var err error
	status := http.StatusOK
	switch {
	case r.Method == "GET" && action == "":
		value, err = v.State(r.Context(), owner, id, true)
	case r.Method == "GET" && action == "status":
		value, err = v.State(r.Context(), owner, id, false)
	case r.Method == "GET" && action == "recoverable":
		value, err = v.Recoverable(r.Context(), owner, id)
	case r.Method == "PUT" && action == "schedule":
		var in struct {
			Schedule snapshots.Schedule `json:"schedule"`
			Confirm  bool               `json:"confirm"`
		}
		if err = decode(w, r, &in); err == nil {
			err = v.SaveSchedule(owner, id, in.Schedule, in.Confirm)
		}
		status = http.StatusNoContent
	case r.Method == "PUT" && action == "automatic":
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		if err = decode(w, r, &in); err == nil {
			if in.Enabled == nil {
				err = errors.New("informe se os snapshots automáticos estão ativados")
			} else {
				err = v.SetAutomatic(owner, id, *in.Enabled)
			}
		}
		status = http.StatusNoContent
	case r.Method == "POST" && action == "":
		value, err = v.StartBackup(owner, id)
		status = http.StatusAccepted
	case r.Method == "POST" && action == "restore":
		var in struct {
			DriveID          string `json:"driveId"`
			PostgresDatabase string `json:"postgresDatabase"`
			Confirm          bool   `json:"confirm"`
		}
		if err = decode(w, r, &in); err == nil {
			value, err = v.StartRestore(owner, id, in.DriveID, "", in.PostgresDatabase, in.Confirm)
		}
		status = http.StatusAccepted
	case r.Method == "POST" && action == "import":
		var in struct {
			Path             string `json:"path"`
			PostgresDatabase string `json:"postgresDatabase"`
			Confirm          bool   `json:"confirm"`
		}
		if err = decode(w, r, &in); err == nil {
			value, err = v.StartRestore(owner, id, "", in.Path, in.PostgresDatabase, in.Confirm)
		}
		status = http.StatusAccepted
	case r.Method == "POST" && action == "download":
		var in struct {
			DriveID string `json:"driveId"`
			Path    string `json:"path"`
		}
		if err = decode(w, r, &in); err == nil {
			value, err = v.StartDownload(owner, id, in.DriveID, in.Path)
		}
		status = http.StatusAccepted
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		databaseError(w, err)
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	write(w, status, value)
}
