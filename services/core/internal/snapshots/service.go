package snapshots

import (
	"cloudforge/core/internal/databases"
	"cloudforge/core/internal/persistence"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Service struct {
	store                 *persistence.Store
	databases             *databases.Manager
	drive                 *drive
	ctx                   context.Context
	cancel                context.CancelFunc
	installation, workDir string

	accountGate sync.RWMutex
	remote      sync.RWMutex
	folders     sync.Mutex
	mu          sync.Mutex
	active      map[string]bool
	closed      bool
	workers     sync.WaitGroup
}

func New(parent context.Context, store *persistence.Store, manager *databases.Manager, installation, dataDir string) (*Service, error) {
	workDir := filepath.Join(dataDir, "snapshot-work")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(workDir); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("diretório de trabalho dos snapshots inválido")
	}
	if err := sweep(workDir); err != nil {
		return nil, err
	}
	if _, err := store.DB.Exec("UPDATE snapshot_jobs SET status='error',detail='Operação interrompida pelo encerramento do Core' WHERE status='running'"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Service{ctx: ctx, cancel: cancel, store: store, databases: manager, installation: installation, workDir: workDir,
		drive: newDrive(store, installation), active: map[string]bool{}}
	s.workers.Add(1)
	go s.run()
	return s, nil
}

func sweep(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "cloudforge-") && strings.HasSuffix(entry.Name(), ".part") {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.workers.Wait()
}

func (s *Service) run() {
	defer s.workers.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.scheduled(s.localNow()); err != nil && s.ctx.Err() == nil {
				log.Print("snapshots: não foi possível verificar os agendamentos")
			}
			if err := s.retryRetention(time.Now()); err != nil && s.ctx.Err() == nil {
				log.Print("snapshots: não foi possível verificar a retenção pendente")
			}
		}
	}
}

type Job struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Progress *int   `json:"progress,omitempty"`
}

type job struct {
	Job
	ctx context.Context
	db  *sql.DB
}

type jobWarning string

func (w jobWarning) Error() string { return string(w) }

func (j *job) persist() {
	raw, _ := json.Marshal(j.Job)
	if _, err := j.db.Exec("UPDATE snapshot_jobs SET status=?,detail=?,updated_at=? WHERE id=?", j.Status, string(raw), time.Now().UnixNano(), j.ID); err != nil {
		log.Print("snapshots: não foi possível salvar o estado da operação")
	}
}

func (j *job) stage(detail string) {
	j.Detail, j.Progress = detail, nil
	j.persist()
}

func (j *job) progress(percent int) {
	if j.Progress == nil || percent >= *j.Progress+5 || (percent == 100 && *j.Progress != 100) {
		j.Progress = &percent
		j.persist()
	}
}

func (s *Service) start(owner, id, stage string, prepare func(*sql.Tx) error, action func(*job) error) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked(owner, id, stage, prepare, action)
}

func (s *Service) startLocked(owner, id, stage string, prepare func(*sql.Tx) error, action func(*job) error) (Job, error) {
	if s.closed || s.ctx.Err() != nil {
		return Job{}, errors.New("o serviço de snapshots está encerrando")
	}
	if s.active[id] || len(s.active) >= 4 {
		return Job{}, databases.ErrConflict
	}
	if _, err := s.databases.SnapshotDatabase(owner, id); err != nil {
		return Job{}, err
	}
	key, err := opaqueID()
	if err != nil {
		return Job{}, err
	}
	initial := Job{ID: key, Status: "running", Detail: stage}
	payload, _ := json.Marshal(initial)
	tx, err := s.store.DB.BeginTx(s.ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	if prepare != nil {
		if err := prepare(tx); err != nil {
			return Job{}, err
		}
	}
	if _, err = tx.Exec("INSERT INTO snapshot_jobs VALUES(?,?,?,?,?)", key, id, initial.Status, string(payload), time.Now().UnixNano()); err != nil {
		return Job{}, err
	}
	if _, err = tx.Exec("DELETE FROM snapshot_jobs WHERE database_id=? AND id NOT IN (SELECT id FROM snapshot_jobs WHERE database_id=? ORDER BY updated_at DESC LIMIT 100)", id, id); err != nil {
		return Job{}, err
	}
	if err = tx.Commit(); err != nil {
		return Job{}, err
	}
	s.active[id] = true
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 6*time.Hour)
		defer cancel()
		j := &job{Job: initial, ctx: ctx, db: s.store.DB}
		err := action(j)
		s.mu.Lock()
		defer s.mu.Unlock()
		defer delete(s.active, id)
		j.Progress = nil
		if err == nil {
			j.Status = "success"
		} else {
			j.Status, j.Detail = "error", err.Error()
			var warning jobWarning
			if errors.As(err, &warning) {
				j.Status = "warning"
			}
			if ctx.Err() != nil {
				j.Detail = "Operação interrompida ou tempo de execução excedido"
			}
		}
		log.Printf("snapshots: operação %s terminou com status %s para o banco %s", j.ID, j.Status, id)
		j.persist()
	}()
	return initial, nil
}

func (s *Service) jobStatus(id string) (*Job, error) {
	var status, raw string
	err := s.store.DB.QueryRow("SELECT status,detail FROM snapshot_jobs WHERE database_id=? ORDER BY updated_at DESC LIMIT 1", id).Scan(&status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j Job
	if json.Unmarshal([]byte(raw), &j) != nil {
		if status == "error" {
			return &Job{Status: status, Detail: raw}, nil
		}
		return nil, errors.New("estado de operação inválido")
	}
	return &j, nil
}

type State struct {
	Drive     driveState `json:"drive"`
	Schedule  Schedule   `json:"schedule"`
	Job       *Job       `json:"job,omitempty"`
	Snapshots []Snapshot `json:"snapshots"`
	ListError string     `json:"listError,omitempty"`
}

func (s *Service) State(ctx context.Context, owner, id string, includeList bool) (State, error) {
	db, err := s.databases.SnapshotDatabase(owner, id)
	if err != nil {
		return State{}, err
	}
	cfg, err := s.schedule(id, s.localNow())
	if err != nil {
		return State{}, err
	}
	j, err := s.jobStatus(id)
	if err != nil {
		return State{}, err
	}
	state := State{Drive: s.drive.status(), Schedule: cfg, Job: j}
	if !includeList {
		return state, nil
	}
	state.Snapshots = []Snapshot{}
	if !state.Drive.Configured || state.Drive.Account == nil {
		return state, nil
	}
	s.accountGate.RLock()
	defer s.accountGate.RUnlock()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	accountID := state.Drive.Account.ID
	folder, err := s.folder(ctx, accountID, db, false)
	if err == nil && folder.ID != "" {
		// A stale response may be displayed briefly during maintenance. Never let it overwrite the reconciled cache.
		s.remote.RLock()
		items, listErr := s.listFolder(ctx, accountID, folder)
		if listErr == nil {
			state.Snapshots, listErr = s.reconcile(ctx, accountID, folder, items)
		}
		s.remote.RUnlock()
		err = listErr
	}
	if err != nil {
		state.ListError = err.Error()
	}
	state.Drive = s.drive.status()
	return state, nil
}

func (s *Service) Connect() (string, error)                { return s.drive.begin(s.ctx, &s.accountGate) }
func (s *Service) DriveStatus() driveState                 { return s.drive.status() }
func (s *Service) ConfigureDrive(client OAuthClient) error { return s.drive.configure(client) }
func (s *Service) Disconnect(ctx context.Context) error {
	if !s.accountGate.TryLock() {
		return errors.New("aguarde as operações do Drive antes de desconectar")
	}
	defer s.accountGate.Unlock()
	return s.drive.disconnect(ctx)
}

func (s *Service) connected() (string, error) {
	state := s.drive.status()
	if !state.Configured || state.Account == nil {
		return "", errors.New("conecte uma conta Google Drive")
	}
	if state.Error == errRevoked.Error() {
		return "", errRevoked
	}
	return state.Account.ID, nil
}

func (s *Service) SaveSchedule(owner, id string, cfg Schedule, confirm bool) error {
	cfg, err := normalizeSchedule(cfg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] {
		return databases.ErrConflict
	}
	db, err := s.databases.SnapshotDatabase(owner, id)
	if err != nil {
		return err
	}
	before, err := s.schedule(id, s.localNow())
	if err != nil {
		return err
	}
	changed := before.Frequency != cfg.Frequency || !slices.Equal(before.Days, cfg.Days) || !slices.Equal(before.Times, cfg.Times)
	raw, _ := json.Marshal(cfg)
	q := "INSERT INTO snapshot_settings(database_id,settings,active_from) VALUES(?,?,?) ON CONFLICT(database_id) DO UPDATE SET settings=excluded.settings"
	if changed {
		q += ",active_from=excluded.active_from,last_slot=NULL"
	}
	active := time.Now().UnixNano()
	if cfg.Limit >= before.Limit {
		_, err = s.store.DB.Exec(q, id, string(raw), active)
		return err
	}
	if !confirm {
		return errors.New("confirme a exclusão das versões acima do novo limite")
	}
	accountID, err := s.connected()
	save := func(tx *sql.Tx, accountID string) error {
		if _, err := tx.Exec(q, id, string(raw), active); err != nil {
			return err
		}
		return markRetention(tx, id, accountID)
	}
	if err != nil {
		if account := s.drive.status().Account; account != nil {
			accountID = account.ID
		}
		tx, err := s.store.DB.BeginTx(s.ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := save(tx, accountID); err != nil {
			return err
		}
		return tx.Commit()
	}
	_, err = s.startLocked(owner, id, "Verificando a retenção", func(tx *sql.Tx) error {
		return save(tx, accountID)
	}, func(j *job) error {
		return s.runRetention(j, accountID, db)
	})
	return err
}

func (s *Service) StartBackup(owner, id string) (Job, error) {
	accountID, err := s.connected()
	if err != nil {
		return Job{}, err
	}
	return s.start(owner, id, "Preparando snapshot", nil, func(j *job) error {
		return s.backup(j, owner, id, "manual", accountID)
	})
}

func (s *Service) backup(j *job, owner, id, mode, accountID string) error {
	ctx := j.ctx
	s.accountGate.RLock()
	defer s.accountGate.RUnlock()
	if _, err := s.drive.accessToken(ctx, accountID, ""); err != nil {
		return err
	}
	db, release, err := s.reserve(ctx, owner, id)
	if err != nil {
		return err
	}
	defer release()
	file, err := s.stageFile()
	if err != nil {
		return errors.New("não foi possível criar o arquivo temporário do snapshot")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	version, err := db.Version()
	if err == nil {
		err = db.Dump(file, j.stage)
	}
	if err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	j.stage("Verificando o arquivo")
	sum, err := fingerprint(file)
	if err != nil || sum.Size == 0 {
		return errors.New("o dump gerado está vazio ou não pôde ser verificado")
	}
	cfg, err := s.schedule(id, s.localNow())
	if err != nil {
		return err
	}

	folder, err := s.folder(ctx, accountID, db.Instance, true)
	if err != nil {
		return err
	}
	format := databases.SnapshotFormat(db.Engine)
	name := "snapshot_" + s.localNow().Format("2006-01-02_15-04") + "." + format
	props := map[string]string{"cfKind": "snapshot", "installation": s.installation, "database": id, "engine": db.Engine,
		"snapshot": j.ID, "mode": mode, "format": format, "version": version, "sha256": sum.SHA256, "md5": sum.MD5}
	meta := map[string]any{"name": name, "mimeType": "application/octet-stream", "parents": []string{folder.ID}, "appProperties": props}
	if err := markRetention(s.store.DB, id, accountID); err != nil {
		return err
	}
	j.stage("Enviando ao Google Drive")
	item, err := s.drive.upload(ctx, accountID, file, sum.Size, meta, j.progress)
	if err != nil && (errors.Is(err, errUploadInterrupted) || errors.Is(err, errDriveNetwork)) {
		found, lookupErr := s.drive.list(ctx, accountID, qvalue(folder.ID)+" in parents and "+property("snapshot", j.ID))
		if lookupErr == nil && len(found) == 1 {
			item, err = found[0], nil
		}
	}
	if err != nil {
		return err
	}
	if _, valid := fileSnapshot(item, folder, ""); !valid || item.Size != sum.Size || item.MD5 != sum.MD5 ||
		item.Props["sha256"] != sum.SHA256 || item.Props["snapshot"] != j.ID {
		return errors.New("o Drive não confirmou a integridade do novo snapshot")
	}
	j.stage("Finalizando snapshot e aplicando retenção")
	s.remote.Lock()
	defer s.remote.Unlock()
	if err := s.retain(ctx, accountID, folder, cfg.Limit, item); err != nil {
		return jobWarning("Snapshot salvo no Drive. A retenção ficou pendente: " + err.Error())
	}
	if err := s.clearRetention(id, accountID); err != nil {
		return jobWarning("Snapshot salvo no Drive. O status da retenção ficou pendente.")
	}
	j.stage("Snapshot realizado com sucesso")
	return nil
}

func (s *Service) StartRestore(owner, id, driveID, localFile string, confirm bool) (Job, error) {
	if !confirm {
		return Job{}, errors.New("confirme a restauração dos dados no banco de destino")
	}
	db, err := s.databases.SnapshotDatabase(owner, id)
	if err != nil {
		return Job{}, err
	}
	var accountID, format string
	if localFile != "" && driveID == "" {
		if !filepath.IsAbs(localFile) {
			return Job{}, errors.New("selecione um arquivo de backup local")
		}
		format, err = databases.SnapshotImportFormat(db.Engine, localFile)
	} else if localFile == "" && driveID != "" {
		accountID, err = s.connected()
	} else {
		return Job{}, errors.New("selecione uma origem para o backup")
	}
	if err != nil {
		return Job{}, err
	}
	return s.start(owner, id, "Preparando restauração", nil, func(j *job) error {
		return s.restore(j, owner, db, accountID, driveID, localFile, format)
	})
}

func (s *Service) restore(j *job, owner string, db databases.Instance, accountID, driveID, localFile, format string) error {
	dest, release, err := s.reserve(j.ctx, owner, db.ID)
	if err != nil {
		return err
	}
	defer release()
	file, err := s.stageFile()
	if err != nil {
		return errors.New("não foi possível preparar o arquivo temporário")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	var version string
	if driveID != "" {
		s.accountGate.RLock()
		defer s.accountGate.RUnlock()
		s.remote.RLock()
		defer s.remote.RUnlock()
		item, err := s.remoteItem(j.ctx, accountID, driveID, db.Engine)
		if err != nil {
			return err
		}
		format, version = item.Props["format"], item.Props["version"]
		j.stage("Baixando e verificando backup")
		if err = s.drive.download(j.ctx, accountID, item, file, j.progress); err != nil {
			return err
		}
	} else {
		j.stage("Lendo arquivo de backup")
		if err := copyLocal(j.ctx, localFile, file, j.progress); err != nil {
			return err
		}
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err = dest.Restore(file, format, version, driveID != "", j.stage); err != nil {
		return fmt.Errorf("restauração no %s: %w", databases.SnapshotEngineName(db.Engine), err)
	}
	j.stage("Snapshot restaurado com sucesso")
	return nil
}

func (s *Service) reserve(ctx context.Context, owner, id string) (*databases.SnapshotDatabase, func(), error) {
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		db, release, err := s.databases.ReserveSnapshot(ctx, owner, id)
		if !errors.Is(err, databases.ErrSnapshotBusy) {
			return db, release, err
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-deadline.C:
			return nil, nil, err
		case <-ticker.C:
		}
	}
}

func copyLocal(ctx context.Context, path string, out io.Writer, progress func(int)) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("selecione um arquivo local regular e não vazio")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("não foi possível abrir o arquivo local")
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(info, current) {
		return errors.New("o arquivo selecionado mudou")
	}
	buf := make([]byte, 256*1024)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return errors.New("falha ao gravar o arquivo temporário")
			}
			size += int64(n)
			if size > info.Size() {
				return errors.New("o arquivo selecionado mudou")
			}
			progress(int(float64(size) * 100 / float64(info.Size())))
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("falha ao ler o backup local")
		}
	}
	if size != info.Size() {
		return errors.New("o arquivo selecionado mudou")
	}
	return nil
}

func (s *Service) StartDownload(owner, id, driveID, destination string) (Job, error) {
	if !filepath.IsAbs(destination) {
		return Job{}, errors.New("selecione o destino do download")
	}
	db, err := s.databases.SnapshotDatabase(owner, id)
	if err != nil {
		return Job{}, err
	}
	accountID, err := s.connected()
	if err != nil {
		return Job{}, err
	}
	return s.start(owner, id, "Preparando download", nil, func(j *job) error {
		s.accountGate.RLock()
		defer s.accountGate.RUnlock()
		s.remote.RLock()
		defer s.remote.RUnlock()
		item, err := s.remoteItem(j.ctx, accountID, driveID, db.Engine)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(strings.ToLower(destination), "."+item.Props["format"]) {
			return errors.New("o nome de destino deve preservar a extensão do snapshot")
		}
		file, err := os.CreateTemp(filepath.Dir(destination), ".cloudforge-snapshot-*.part")
		if err != nil {
			return errors.New("não foi possível gravar no diretório escolhido")
		}
		defer os.Remove(file.Name())
		defer file.Close()
		j.stage("Baixando snapshot")
		if err = s.drive.download(j.ctx, accountID, item, file, j.progress); err != nil {
			return err
		}
		if err = file.Sync(); err != nil {
			return errors.New("não foi possível confirmar a gravação do snapshot")
		}
		if err = file.Close(); err != nil {
			return errors.New("não foi possível fechar o arquivo de download")
		}
		if err = os.Rename(file.Name(), destination); err != nil {
			return errors.New("não foi possível salvar o snapshot no destino")
		}
		j.stage("Snapshot baixado com sucesso")
		return nil
	})
}
