package snapshots

import (
	"cloudforge/core/internal/databases"
	"database/sql"
	"errors"
	"time"
)

type sqlExecutor interface {
	Exec(string, ...any) (sql.Result, error)
}

const retentionRetry = time.Minute

func markRetention(db sqlExecutor, id, accountID string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO snapshot_retention VALUES(?,?,?)", id, accountID, time.Now().Add(retentionRetry).Unix())
	return err
}

func (s *Service) clearRetention(id, accountID string) error {
	_, err := s.store.DB.Exec("DELETE FROM snapshot_retention WHERE database_id=? AND (account_id=? OR account_id='')", id, accountID)
	return err
}

func (s *Service) runRetention(j *job, accountID string, db databases.Instance) error {
	s.accountGate.RLock()
	defer s.accountGate.RUnlock()
	folder, err := s.folder(j.ctx, accountID, db, false)
	if err != nil {
		return err
	}
	if folder.ID != "" {
		cfg, err := s.schedule(db.ID, s.localNow())
		if err != nil {
			return err
		}
		s.remote.Lock()
		err = s.retain(j.ctx, accountID, folder, cfg.Limit)
		s.remote.Unlock()
		if err != nil {
			return err
		}
	}
	if err := s.clearRetention(db.ID, accountID); err != nil {
		return err
	}
	j.stage("Limite de versões aplicado")
	return nil
}

func (s *Service) retryRetention(now time.Time) error {
	accountID, err := s.connected()
	if err != nil {
		return nil
	}
	rows, err := s.store.DB.Query("SELECT DISTINCT h.id,h.owner FROM snapshot_retention r JOIN hosted_databases h ON h.id=r.database_id WHERE (r.account_id=? OR r.account_id='') AND r.retry_at<=?", accountID, now.Unix())
	if err != nil {
		return err
	}
	type origin struct{ id, owner string }
	var pending []origin
	for rows.Next() {
		var item origin
		if err := rows.Scan(&item.id, &item.owner); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range pending {
		db, err := s.databases.SnapshotDatabase(item.owner, item.id)
		if errors.Is(err, databases.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		_, err = s.start(item.owner, item.id, "Reaplicando retenção", func(tx *sql.Tx) error {
			result, err := tx.Exec("UPDATE snapshot_retention SET retry_at=? WHERE database_id=? AND (account_id=? OR account_id='') AND retry_at<=?", now.Add(retentionRetry).Unix(), item.id, accountID, now.Unix())
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return databases.ErrConflict
			}
			return nil
		}, func(j *job) error { return s.runRetention(j, accountID, db) })
		if err != nil && !errors.Is(err, databases.ErrConflict) && !errors.Is(err, databases.ErrNotFound) {
			return err
		}
	}
	return nil
}
