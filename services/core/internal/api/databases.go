package api

import (
	"cloudforge/core/internal/databases"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (s *Server) databaseRoutes(w http.ResponseWriter, r *http.Request, owner string, local bool) {
	if s.Databases == nil {
		fail(w, 503, errors.New("serviço de bancos de dados indisponível"))
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 && parts[2] == "lan" && parts[3] == "firewall" {
		if !local || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var input struct {
			Confirmed bool `json:"confirmed"`
		}
		if err := decode(w, r, &input); err != nil {
			fail(w, 400, err)
			return
		}
		if !input.Confirmed {
			fail(w, 400, errors.New("confirme a alteração do firewall"))
			return
		}
		if err := s.Databases.RequestLANFirewall(); err != nil {
			databaseError(w, err)
			return
		}
		write(w, http.StatusAccepted, map[string]bool{"ok": true})
		return
	}
	if len(parts) == 2 {
		switch r.Method {
		case "GET":
			list, err := s.Databases.List(r.Context(), owner)
			if err != nil {
				databaseError(w, err)
				return
			}
			if !local {
				list.LANFirewall = ""
			}
			write(w, 200, list)
		case "POST":
			var in databases.Input
			if err := decode(w, r, &in); err != nil {
				fail(w, 400, err)
				return
			}
			instance, err := s.Databases.Create(r.Context(), owner, in)
			if err != nil {
				databaseError(w, err)
				return
			}
			write(w, 202, instance)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	id := parts[2]
	switch {
	case len(parts) == 3 && r.Method == "GET":
		window := time.Hour
		switch r.URL.Query().Get("period") {
		case "10m":
			window = 10 * time.Minute
		case "24h":
			window = 24 * time.Hour
		}
		instance, metrics, err := s.Databases.Detail(owner, id, window)
		if err != nil {
			databaseError(w, err)
			return
		}
		write(w, 200, map[string]any{"database": instance, "metrics": metrics})
	case len(parts) == 3 && r.Method == "PATCH":
		var change databases.Change
		if err := decode(w, r, &change); err != nil {
			fail(w, 400, err)
			return
		}
		instance, err := s.Databases.Change(owner, id, change)
		if err != nil {
			databaseError(w, err)
			return
		}
		write(w, 202, instance)
	case len(parts) == 4 && parts[3] == "credentials" && r.Method == "POST":
		w.Header().Set("Cache-Control", "no-store")
		credentials, err := s.Databases.Reveal(owner, id)
		if err != nil {
			databaseError(w, err)
			return
		}
		write(w, 200, credentials)
	case len(parts) == 5 && parts[3] == "certificates" && r.Method == "GET":
		w.Header().Set("Cache-Control", "no-store")
		content, filename, err := s.Databases.Certificate(owner, id, parts[4])
		if err != nil {
			databaseError(w, err)
			return
		}
		// Keep the transport compatible with both the web and Electron JSON clients.
		write(w, 200, map[string]string{"filename": filename, "content": string(content)})
	default:
		http.NotFound(w, r)
	}
}

func databaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, databases.ErrNotFound):
		fail(w, 404, err)
	case errors.Is(err, databases.ErrConflict):
		fail(w, 409, err)
	case errors.Is(err, databases.ErrUnavailable):
		fail(w, 503, err)
	case errors.Is(err, context.Canceled):
		fail(w, 408, err)
	default:
		fail(w, 400, err)
	}
}
