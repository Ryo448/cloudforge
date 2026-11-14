package databases

import (
	"context"
	"errors"
	"time"
)

func (m *Manager) rotateTLS(ctx context.Context, r *record, s secret, runtime containerState, exists bool) error {
	if !exists || !runtime.Local || !runtime.TLS || s.TLS == nil {
		r.Action = "reconcile"
		return errors.New("configuração TLS indisponível; o banco será reconciliado")
	}
	previous := s.TLS
	candidate, err := m.issueTLS(*r)
	if err == nil {
		runtime, err = m.restartWithTLS(ctx, *r, s, candidate)
		if err == nil {
			newRecord, newSecrets := *r, s
			newSecrets.TLS = candidate
			newRecord.Action, newRecord.State, newRecord.Issue = "", "running", ""
			newRecord.StartedAt, newRecord.CredentialsPending = &runtime.StartedAt, s.Reveal
			m.mu.Lock()
			err = m.saveRecordAndSecrets(newRecord, newSecrets)
			if err == nil {
				*r = newRecord
				m.ready[r.ID] = runtime.StartedAt.String()
			}
			m.mu.Unlock()
			if err == nil {
				return nil
			}
		}
	}
	// The previous bundle stays committed until the new endpoint passes both
	// probes. Recover it even if the worker's deadline expired during rotation.
	if ctx.Err() == context.Canceled {
		return ctx.Err() // the persisted action is resumed after Core restarts
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 100*time.Second)
	defer cancel()
	runtime, err = m.restartWithTLS(recovery, *r, s, previous)
	if err != nil {
		r.Action = "restart"
		return errors.New("a redefinição falhou e o certificado anterior ainda não pôde ser reativado")
	}
	r.Action, r.State, r.Issue = "", "running", "A redefinição falhou. O certificado anterior foi restaurado."
	r.StartedAt, r.CredentialsPending = &runtime.StartedAt, s.Reveal
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ready[r.ID] = runtime.StartedAt.String()
	return m.save(*r)
}

func (m *Manager) restartWithTLS(ctx context.Context, r record, s secret, material *serverTLS) (containerState, error) {
	if err := material.validate(r); err != nil {
		return containerState{}, err
	}
	if err := m.docker.lifecycle(ctx, r, "stop", "--time", "15"); err != nil {
		return containerState{}, err
	}
	if err := m.docker.lifecycle(ctx, r, "start"); err != nil {
		return containerState{}, err
	}
	runtime, _, err := m.docker.inspect(ctx, r)
	if err != nil {
		return containerState{}, err
	}
	if !runtime.Local || runtime.LocalPort != r.ReservedPort {
		return containerState{}, errors.New("mapeamento local do banco inválido")
	}
	if err = m.docker.configure(ctx, r, s, material); err != nil {
		return containerState{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		if err = m.docker.ready(ctx, r, material, s.Password, runtime.LocalPort); err == nil {
			if err = m.docker.clientTLSReady(ctx, r, material, s.Admin, runtime.LocalPort); err == nil {
				return runtime, nil
			}
		}
		select {
		case <-ctx.Done():
			return containerState{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
