package databases

import (
	"cloudforge/core/internal/persistence"
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound    = errors.New("banco de dados não encontrado")
	ErrConflict    = errors.New("a operação conflita com o estado atual do banco de dados")
	ErrUnavailable = errors.New("Docker local (Engine 28 ou superior, em modo Linux) indisponível")
)

type Manager struct {
	db            *sql.DB
	vault         persistence.EnvironmentVault
	localKey      [32]byte
	docker        docker
	dockerReady   bool
	dockerChecked bool
	dockerStatus  dockerStatus
	keyMu         sync.Mutex
	mu            sync.Mutex
	active        map[string]bool
	ready         map[string]string
	latest        map[string]Metric
	retryAt       map[string]time.Time
	jobs          chan struct{}
	wake          chan struct{}
}

func New(store *persistence.Store, installationKey string) *Manager {
	return &Manager{db: store.DB, vault: store.EnvironmentVault,
		localKey: deriveLocalKey(installationKey),
		active:   map[string]bool{}, ready: map[string]string{}, latest: map[string]Metric{}, retryAt: map[string]time.Time{}, jobs: make(chan struct{}, 4), wake: make(chan struct{}, 1)}
}

type View struct {
	Databases    []Instance   `json:"databases"`
	Docker       bool         `json:"docker"`
	DockerStatus dockerStatus `json:"dockerStatus,omitempty"`
	MemoryLimit  int          `json:"memoryLimit"`
	CPU          float64      `json:"cpu"`
}

func (m *Manager) List(ctx context.Context, owner string) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	records, err := m.records(owner)
	view := View{Databases: []Instance{}, Docker: m.dockerReady, DockerStatus: m.dockerStatus, MemoryLimit: memoryLimit, CPU: cpuLimit}
	for _, r := range records {
		view.Databases = append(view.Databases, m.view(r))
	}
	return view, err
}

func (m *Manager) view(r record) Instance {
	r.URL = r.connection("")
	r.CanRotateTLS = m.dockerReady && canRotateTLS(r) && (m.tlsAvailable(r) || r.State == "error")
	r.TLSValidated = m.tlsAvailable(r)
	if r.State == "running" && !r.TLSValidated {
		r.State, r.CredentialsPending = "starting", false
	}
	if sample, ok := m.latest[r.ID]; ok && r.State == "running" {
		r.Latest = &sample
	}
	return r.Instance
}

func (m *Manager) tlsAvailable(r record) bool {
	return r.TLSValidated && r.Action == "" &&
		(r.State == "stopped" || (r.State == "running" && m.ready[r.ID] != ""))
}

func canRotateTLS(r record) bool {
	return r.Desired == "running" && r.TLSValidated && r.Action == "" &&
		(r.State == "running" || r.State == "error")
}

func (m *Manager) Detail(owner, id string, window time.Duration) (Instance, []Metric, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.get(owner, id)
	if err != nil {
		return Instance{}, nil, err
	}
	samples, err := m.history(r, window)
	return m.view(r), samples, err
}

func (m *Manager) Create(ctx context.Context, owner string, input Input) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.dockerReady {
		return Instance{}, ErrUnavailable
	}
	name, err := validName(input.Name)
	if err != nil {
		return Instance{}, err
	}
	if _, ok := engines[input.Engine]; !ok {
		return Instance{}, errors.New("tecnologia inválida")
	}
	memoryMB := defaultMemoryMB
	if input.MemoryMB != nil {
		memoryMB = *input.MemoryMB
	}
	if memoryMB < 0 || memoryMB > 1048576 {
		return Instance{}, errors.New("informe um limite de memória entre 0 e 1048576 MB")
	}
	if memoryMB > 0 && memoryMB < 6 {
		return Instance{}, errors.New("o limite de memória deve ser 0 ou pelo menos 6 MB")
	}
	id, err := randomHex(16)
	if err != nil {
		return Instance{}, err
	}
	r := record{Instance: Instance{ID: id, Name: name, Engine: input.Engine, Host: localHost,
		MemoryMB: &memoryMB, Desired: "running", State: "starting", CreatedAt: time.Now().UTC()}, Owner: owner, Action: "create"}
	raw, err := r.encode()
	if err != nil {
		return Instance{}, err
	}
	if _, err = m.db.ExecContext(ctx, "INSERT INTO hosted_databases(id,owner,engine,data) VALUES(?,?,?,?)", r.ID, owner, r.Engine, raw); err != nil {
		return Instance{}, conflict(err)
	}
	m.notify()
	return r.Instance, nil
}

func conflict(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	return err
}

type Change struct {
	Action       string `json:"action"`
	Name         string `json:"name"`
	Confirmation string `json:"confirmation"`
}

func (m *Manager) Change(owner, id string, change Change) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.get(owner, id)
	if err != nil {
		return Instance{}, err
	}
	start := change.Action == "start" || change.Action == "restart" || change.Action == "retry"
	if m.active[id] || (r.Action != "" && r.State != "error") ||
		(r.Action == "delete" && change.Action != "delete" && !start) {
		return Instance{}, ErrConflict
	}
	switch change.Action {
	case "rename":
		r.Name, err = validName(change.Name)
		if err != nil {
			return Instance{}, err
		}
	case "delete":
		if change.Confirmation != r.Name {
			return Instance{}, errors.New("digite o nome do banco para confirmar a exclusão")
		}
		r.Desired, r.State, r.Action = "stopped", "stopping", "delete"
	case "start", "restart", "retry":
		recovering := r.Action == "delete" || r.Action == "recover"
		r.Desired, r.State, r.Action = "running", "starting", "reconcile"
		if recovering {
			r.Action = "recover"
		} else if change.Action == "restart" {
			r.Action = "restart"
		}
	case "stop":
		r.Desired, r.State, r.Action = "stopped", "stopping", "reconcile"
	case "password":
		if r.State != "running" || !m.tlsAvailable(r) {
			return Instance{}, ErrConflict
		}
		r.Action, r.State, r.CredentialsPending = "password", "restarting", false
	case "certificate":
		if !m.dockerReady {
			return Instance{}, ErrUnavailable
		}
		if !canRotateTLS(r) {
			return Instance{}, ErrConflict
		}
		r.Action, r.State = "certificate", "restarting"
	default:
		return Instance{}, errors.New("operação inválida")
	}
	if err = m.save(r); err != nil {
		return Instance{}, conflict(err)
	}
	delete(m.retryAt, id)
	m.notify()
	return m.view(r), nil
}

func (m *Manager) Reveal(owner, id string) (Credentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.get(owner, id)
	if err != nil {
		return Credentials{}, err
	}
	if !r.CredentialsPending || r.State != "running" || !m.tlsAvailable(r) {
		return Credentials{}, ErrConflict
	}
	s, err := m.secrets(r)
	if err != nil {
		return Credentials{}, err
	}
	if !s.Reveal {
		return Credentials{}, ErrNotFound
	}
	s.Reveal = false
	r.CredentialsPending = false
	if err = m.saveRecordAndSecrets(r, s); err != nil {
		return Credentials{}, err
	}
	return Credentials{Password: s.Password, URL: r.connection(s.Password)}, nil
}

func (m *Manager) notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) Run(ctx context.Context) {
	ctx, stopMonitor := context.WithCancel(ctx)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		m.monitorDocker(ctx)
	}()
	defer func() { stopMonitor(); <-monitorDone }()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	var lastID string
	for {
		m.mu.Lock()
		if !m.dockerChecked {
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-m.wake:
				continue
			}
		}
		available, starting := m.dockerReady, m.dockerStatus == dockerStarting
		records, err := m.records("")
		if err != nil {
			log.Print("bancos: falha ao listar instâncias")
		}
		first := sort.Search(len(records), func(i int) bool { return records[i].ID > lastID })
		for i := range records {
			r := records[(first+i)%len(records)]
			// A failed deletion requires a new confirmation, even after restarting the Core.
			if r.Action == "delete" && r.State == "error" {
				continue
			}
			if m.active[r.ID] || time.Now().Before(m.retryAt[r.ID]) {
				continue
			}
			if !available {
				if !starting && (r.State != "error" || r.Issue != ErrUnavailable.Error()) {
					r.State, r.Issue = "error", ErrUnavailable.Error()
					_ = m.save(r)
				}
				delete(m.latest, r.ID)
				delete(m.ready, r.ID)
				continue
			}
			select {
			case m.jobs <- struct{}{}:
			default:
				continue
			}
			m.active[r.ID] = true
			lastID = r.ID
			workers.Add(1)
			go func(r record) {
				defer workers.Done()
				ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
				defer cancel()
				err := m.reconcile(ctx, &r)
				m.mu.Lock()
				defer m.mu.Unlock()
				if err != nil && ctx.Err() != context.Canceled {
					m.retryAt[r.ID] = time.Now().Add(30 * time.Second)
					delete(m.latest, r.ID)
					delete(m.ready, r.ID)
					r.State, r.Issue = "error", err.Error()
					r.CredentialsPending = false
					if err := m.save(r); err != nil {
						log.Print("bancos: falha ao persistir estado")
					}
				} else {
					delete(m.retryAt, r.ID)
				}
				delete(m.active, r.ID)
				<-m.jobs
			}(r)
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.wake:
		}
	}
}

func (m *Manager) monitorDocker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var attempted bool
	var startupDeadline time.Time
	for {
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		status := m.docker.status(probe)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if status != dockerUnavailable {
			attempted = false
			startupDeadline = time.Time{}
		} else if !attempted {
			attempted = true
			m.setDockerStatus(dockerStarting)
			if startLocalDocker(ctx) {
				startupDeadline = time.Now().Add(90 * time.Second)
			}
		}
		if status == dockerUnavailable && time.Now().Before(startupDeadline) {
			status = dockerStarting
		}
		m.setDockerStatus(status)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) setDockerStatus(status dockerStatus) {
	m.mu.Lock()
	changed := m.dockerStatus != status
	m.dockerStatus = status
	m.dockerReady, m.dockerChecked = status == dockerRunning, true
	m.mu.Unlock()
	if changed {
		m.notify()
	}
}

func (m *Manager) reconcile(ctx context.Context, r *record) error {
	if r.Action == "delete" {
		if err := m.docker.remove(ctx, *r); err != nil {
			return err
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.latest, r.ID)
		delete(m.ready, r.ID)
		_, err := m.db.Exec("DELETE FROM hosted_databases WHERE id=?", r.ID)
		return err
	}

	runtime, exists, err := m.docker.inspect(ctx, *r)
	if err != nil {
		return err
	}
	s, err := m.secrets(*r)
	if err != nil {
		return err
	}
	s, err = m.backfillClientTLS(*r, s)
	if err != nil {
		return err
	}
	if r.Desired == "stopped" {
		if exists && runtime.Running {
			if err = m.docker.lifecycle(ctx, *r, "stop", "--time", "15"); err != nil {
				return err
			}
		}
		r.Action, r.State, r.Issue, r.StartedAt = "", "stopped", "", nil
		m.mu.Lock()
		delete(m.ready, r.ID)
		delete(m.latest, r.ID)
		m.mu.Unlock()
		return m.save(*r)
	}

	if s.Password == "" {
		s.Password, err = randomHex(32)
		if err != nil {
			return err
		}
		s.Admin, err = randomHex(32)
		if err != nil {
			return err
		}
		s.Reveal = true
		if err = m.saveSecrets(*r, s); err != nil {
			return err
		}
	}
	if r.Action == "certificate" {
		return m.rotateTLS(ctx, r, s, runtime, exists)
	}
	if s.TLS == nil {
		s.TLS, err = m.issueTLS(*r)
		if err != nil {
			return err
		}
		if err = m.saveSecrets(*r, s); err != nil {
			return err
		}
	}
	if err = s.TLS.validate(*r); err != nil {
		return err
	}
	if _, attached := runtime.Networks[containerName(*r)]; exists && r.Engine == "postgresql" && runtime.LocalConfigured && runtime.TLS && !attached {
		// Docker can keep forwarding to the previous endpoint after a live
		// network change. Stop before reattaching if the port is unreachable.
		if runtime.Running && (!runtime.Local || m.docker.ready(ctx, *r, s.TLS, s.Password, runtime.LocalPort) != nil) {
			if err = m.docker.lifecycle(ctx, *r, "stop", "--time", "15"); err != nil {
				return err
			}
			runtime.Running = false
			runtime.Local = true
		}
		if err = m.docker.reconnectManagedNetwork(ctx, *r); err != nil {
			return err
		}
	}
	if exists && (!runtime.Local || !runtime.TLS) {
		if err = m.docker.stageLegacy(ctx, *r, runtime); err != nil {
			return err
		}
		exists = false
	}
	if exists && runtime.Running && r.Action == "restart" {
		if err = m.docker.lifecycle(ctx, *r, "stop", "--time", "15"); err != nil {
			return err
		}
		runtime.Running = false
	}
	if !exists || !runtime.Running {
		if r.Engine == "postgresql" {
			addresses, err := postgresLANAddresses()
			if err != nil {
				return err
			}
			material, err := m.postgresTLSForLAN(*r, s.TLS, addresses)
			if err != nil {
				return err
			}
			if material != s.TLS {
				s.TLS = material
				if err = m.saveSecrets(*r, s); err != nil {
					return err
				}
			}
		}
		if runtime, err = m.start(ctx, r, exists); err != nil {
			return err
		}
	}
	if runtime.LocalPort == 0 || runtime.LocalPort != r.ReservedPort {
		return errors.New("mapeamento local do banco inválido")
	}
	stamp := runtime.StartedAt.String()
	m.mu.Lock()
	ready := m.ready[r.ID] == stamp
	m.mu.Unlock()
	if !ready {
		if err = m.docker.configure(ctx, *r, s, s.TLS); err != nil {
			return err
		}
	} else if r.Engine == "postgresql" {
		if err = m.docker.reconcilePostgresHBA(ctx, *r, false); err != nil {
			return err
		}
	}
	if r.Action == "password" || s.PendingPassword != "" {
		if s.PendingPassword == "" {
			s.PendingPassword, err = randomHex(32)
			if err != nil {
				return err
			}
			if err = m.saveSecrets(*r, s); err != nil {
				return err
			}
		}
	}
	password := s.Password
	if s.PendingPassword != "" {
		password = s.PendingPassword
	}
	legacyMySQL := false
	if r.Engine == "mysql" {
		_, legacyMySQL, err = m.docker.inspectNamed(ctx, *r, containerName(*r)+"-legacy")
		if err != nil {
			return err
		}
	}

	if !ready || password != s.Password || legacyMySQL {
		deadline, cancel := context.WithTimeout(ctx, 100*time.Second)
		defer cancel()
		for {
			if legacyMySQL {
				err = m.docker.setPassword(deadline, *r, s, password)
				legacyMySQL = err != nil
			}
			if !legacyMySQL {
				err = m.docker.ready(deadline, *r, s.TLS, password, runtime.LocalPort)
			}
			if err == nil {
				break
			}
			if r.Engine != "postgresql" || errors.Is(err, errPasswordRejected) {
				_ = m.docker.setPassword(deadline, *r, s, password)
			}
			select {
			case <-deadline.Done():
				return errors.New("não foi possível validar TLS e autenticação na porta local do banco")
			case <-time.After(2 * time.Second):
			}
		}
		if r.Engine == "postgresql" {
			if err = m.docker.ensurePostgresAdmin(deadline, *r, s.Admin); err != nil {
				return err
			}
		}
		if s.PendingPassword != "" {
			if err = m.docker.ready(deadline, *r, s.TLS, s.Password, runtime.LocalPort); !errors.Is(err, errPasswordRejected) {
				return errors.New("não foi possível confirmar a invalidação da senha anterior")
			}
			s.Password, s.PendingPassword, s.Reveal = password, "", true
			r.Action = ""
			if err = m.saveRecordAndSecrets(*r, s); err != nil {
				r.Action = "password"
				return err
			}
		}
		if err = m.docker.clientTLSReady(deadline, *r, s.TLS, s.Admin, runtime.LocalPort); err != nil {
			return errors.New("não foi possível validar o certificado TLS de cliente na porta local do banco")
		}
		m.mu.Lock()
		m.ready[r.ID] = stamp
		m.mu.Unlock()
	} else if err = m.docker.ready(ctx, *r, s.TLS, password, runtime.LocalPort); err != nil {
		return errors.New("falha ao verificar TLS e autenticação do banco local")
	}

	if err = m.docker.restoreNetworks(ctx, *r, runtime); err != nil {
		return err
	}
	if err = m.docker.removeLegacy(ctx, *r); err != nil {
		return err
	}
	if !ready || r.Action != "" || r.State != "running" {
		r.Issue = ""
	}
	r.State, r.Action, r.StartedAt, r.TLSValidated = "running", "", &runtime.StartedAt, true
	r.Port = runtime.LocalPort
	if sample, err := m.docker.metrics(ctx, *r); err == nil {
		m.mu.Lock()
		previous, ok := m.latest[r.ID]
		m.latest[r.ID] = sample
		m.mu.Unlock()
		if !ok || time.UnixMilli(previous.Time).Truncate(time.Minute) != time.UnixMilli(sample.Time).Truncate(time.Minute) {
			if err = m.addMetric(*r, sample); err != nil {
				log.Print("bancos: falha ao persistir métricas")
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.secrets(*r)
	if err != nil {
		return err
	}
	r.CredentialsPending = current.Reveal
	return m.save(*r)
}

func (m *Manager) start(ctx context.Context, r *record, exists bool) (containerState, error) {
	for attempt := 0; attempt < 12; attempt++ {
		if r.ReservedPort == 0 {
			if err := m.allocatePort(r); err != nil {
				return containerState{}, err
			}
		}
		if !exists {
			if err := m.docker.create(ctx, *r); err != nil {
				return containerState{}, err
			}
		}
		err := m.docker.lifecycle(ctx, *r, "start")
		if err == nil {
			state, _, err := m.docker.inspect(ctx, *r)
			return state, err
		}
		if !errors.Is(err, errPortInUse) || r.Port != 0 {
			return containerState{}, err
		}
		if err = m.docker.lifecycle(ctx, *r, "rm"); err != nil {
			return containerState{}, err
		}
		if err = m.setPort(r, 0); err != nil {
			return containerState{}, err
		}
		exists = false
	}
	return containerState{}, errPortInUse
}

func (m *Manager) allocatePort(r *record) error {
	for attempt := 0; attempt < 32; attempt++ {
		listener, err := net.Listen("tcp4", databaseBindHost(r.Engine)+":0")
		if err != nil {
			return err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		err = m.setPort(r, port)
		_ = listener.Close()
		if !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return errors.New("não há portas locais disponíveis")
}
