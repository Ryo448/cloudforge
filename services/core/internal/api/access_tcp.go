package api

import (
	"errors"
	"net/http"
)

func (s *Server) accessTCPRoutes(w http.ResponseWriter, r *http.Request, parts []string) {
	if _, err := s.Store.Admin(); err != nil {
		fail(w, http.StatusUnauthorized, errors.New("configure o administrador local"))
		return
	}
	if s.Cloud == nil || s.Cloud.AccessTCP == nil {
		fail(w, http.StatusServiceUnavailable, errors.New("serviço de conexões TCP indisponível"))
		return
	}
	tcp := s.Cloud.AccessTCP
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		write(w, http.StatusOK, tcp.List())
		return
	case len(parts) == 2 && r.Method == http.MethodPost:
		var input struct {
			Hostname string `json:"hostname"`
			Port     int    `json:"port"`
		}
		if err := decode(w, r, &input); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		if err := tcp.Add(input.Hostname, input.Port); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
	case len(parts) == 3 && r.Method == http.MethodPatch:
		var input struct {
			Action string `json:"action"`
		}
		if err := decode(w, r, &input); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		if input.Action != "start" && input.Action != "stop" {
			fail(w, http.StatusBadRequest, errors.New("operação inválida"))
			return
		}
		if err := tcp.Change(parts[2], input.Action); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
	case len(parts) == 3 && r.Method == http.MethodDelete:
		if err := tcp.Change(parts[2], "delete"); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	write(w, http.StatusOK, tcp.List())
}
