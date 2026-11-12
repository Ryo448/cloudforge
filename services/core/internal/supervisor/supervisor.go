package supervisor

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/processes"
	"cloudforge/core/internal/projects"
	"cloudforge/core/internal/runtimes"
	"context"
	"errors"
	"fmt"
	"github.com/shirou/gopsutil/v4/process"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type job struct {
	mu       sync.Mutex
	op       sync.Mutex
	p        model.Project
	state    model.Runtime
	usage    []model.UsageSample
	logs     *Logbook
	cancel   context.CancelFunc
	done     chan struct{}
	tree     *processes.Tree
	terminal *terminalSession
	active   bool
	blocked  string
	removing bool
}
type Supervisor struct {
	ops    sync.WaitGroup
	mu     sync.RWMutex
	store  persistence.Repository
	jobs   map[string]*job
	logdir string
	ctx    context.Context
	cancel context.CancelFunc
}

func New(s persistence.Repository, dataDir string) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Supervisor{store: s, jobs: map[string]*job{}, logdir: filepath.Join(dataDir, "logs"), ctx: ctx, cancel: cancel}
}
func (s *Supervisor) Add(p model.Project) {
	j := &job{p: p, state: model.Runtime{ProjectID: p.ID, ActualState: "offline", Health: "offline"}, logs: NewLogbook(s.logdir, p.ID)}
	s.mu.Lock()
	s.jobs[p.ID] = j
	s.mu.Unlock()
}
func (s *Supervisor) Restore() error {
	ps, e := s.store.Projects()
	if e != nil {
		return e
	}
	for _, p := range ps {
		s.Add(p)
		if old, e := s.store.PreviousRuntime(p.ID); e == nil && old.PID > 0 && old.StartedAt != nil {
			if proc, e := process.NewProcess(int32(old.PID)); e == nil {
				created, e := proc.CreateTime()
				if e == nil && abs(created-old.StartedAt.UnixMilli()) < 3000 {
					j, _ := s.get(p.ID)
					executable, exeError := proc.Exe()
					if old.ProcessCreatedAt == created && old.Executable != "" && exeError == nil && strings.EqualFold(executable, old.Executable) {
						if recoverError := processes.StopRecovered(old.PID); recoverError == nil {
							j.logs.Add("system", "Execução anterior identificada e encerrada antes da recuperação.")
							if p.DesiredState == "running" {
								_, _ = s.Action(p.ID, "start")
							}
							continue
						}
					}
					j.mu.Lock()
					j.state.ActualState = "error"
					j.state.LastError = "Processo da execução anterior ainda existe. Encerre-o antes de iniciar para evitar duplicação."
					j.blocked = j.state.LastError
					j.mu.Unlock()
					j.logs.Add("system", "Recuperação bloqueou instância duplicada.")
					continue
				}
			}
		}
		if p.DesiredState == "running" {
			_, _ = s.Action(p.ID, "start")
		}
	}
	go s.monitor()
	return nil
}
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
func (s *Supervisor) get(id string) (*job, error) {
	s.mu.RLock()
	j := s.jobs[id]
	s.mu.RUnlock()
	if j == nil {
		return nil, errors.New("projeto não encontrado")
	}
	return j, nil
}
func (s *Supervisor) Action(id, action string) (string, error) {
	j, e := s.get(id)
	if e != nil {
		return "", e
	}
	if action != "start" && action != "stop" && action != "restart" {
		return "", errors.New("ação inválida")
	}
	j.mu.Lock()
	blocked := j.blocked
	j.mu.Unlock()
	if blocked != "" {
		return "", errors.New(blocked)
	}
	if !j.op.TryLock() {
		return "", errors.New("há uma operação em andamento neste projeto")
	}
	j.mu.Lock()
	removing := j.removing
	j.mu.Unlock()
	if removing {
		j.op.Unlock()
		return "", errors.New("projeto em exclusão ou já excluído")
	}
	op := auth.ID()
	if e = s.store.Operation(op, "process", "pending", map[string]string{"projectId": id, "action": action}, ""); e != nil {
		j.op.Unlock()
		return "", e
	}
	desired := "running"
	if action == "stop" {
		desired = "stopped"
	}
	if e = s.store.Desired(id, desired); e != nil {
		j.op.Unlock()
		return "", e
	}
	j.mu.Lock()
	j.p.DesiredState = desired
	if action == "restart" {
		j.state.ActualState = "restarting"
	} else if action == "stop" {
		j.state.ActualState = "stopping"
	} else if !j.active {
		j.state.ActualState = "starting"
	}
	j.mu.Unlock()
	s.ops.Add(1)
	go func() {
		defer s.ops.Done()
		if action == "stop" || action == "restart" {
			s.stop(j)
		}
		if action != "stop" {
			s.launch(j)
			j.op.Unlock()
			s.observeOperation(j, op, id, action)
			return
		}
		_ = s.store.Operation(op, "process", "complete", map[string]string{"projectId": id, "action": action}, "")
		j.op.Unlock()
	}()
	return op, nil
}
func (s *Supervisor) SetPort(id string, port int) error {
	j, err := s.get(id)
	if err != nil {
		return err
	}
	if !j.op.TryLock() {
		return errors.New("aguarde a operação em andamento neste projeto")
	}
	defer j.op.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.removing {
		return errors.New("projeto em exclusão ou já excluído")
	}
	p := j.p
	p.Port, p.UpdatedAt = port, time.Now().UTC()
	if err := s.store.SaveProjectPort(p); err != nil {
		return err
	}
	j.p = p
	if port == 0 {
		j.state.Health = "not-configured"
	}
	return nil
}

func (s *Supervisor) SetStartup(id string, command []string, entry string, args []string) error {
	j, err := s.get(id)
	if err != nil {
		return err
	}
	if !j.op.TryLock() {
		return errors.New("aguarde a operação em andamento neste projeto")
	}
	defer j.op.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.removing {
		return errors.New("projeto em exclusão ou já excluído")
	}
	p := j.p
	if p.Runtime == "node" {
		p.StartCommand = command
	} else {
		p.EntryPoint, p.Arguments = entry, args
	}
	if err := projects.ValidateStartup(&p); err != nil {
		return err
	}
	p.UpdatedAt = time.Now().UTC()
	if err := s.store.SaveProject(p); err != nil {
		return err
	}
	j.p = p
	return nil
}

func (s *Supervisor) Remove(id string) error {
	j, err := s.get(id)
	if err != nil {
		return err
	}
	if !j.op.TryLock() {
		return errors.New("aguarde a operação em andamento neste projeto")
	}
	defer j.op.Unlock()
	j.mu.Lock()
	if j.removing || j.blocked != "" {
		j.mu.Unlock()
		return errors.New("não foi possível confirmar que o projeto pode ser encerrado")
	}
	j.removing = true
	j.mu.Unlock()
	deleted := false
	defer func() {
		if !deleted {
			j.mu.Lock()
			j.removing = false
			j.mu.Unlock()
		}
	}()
	if err = s.store.Desired(id, "stopped"); err != nil {
		return err
	}
	j.mu.Lock()
	j.p.DesiredState = "stopped"
	j.mu.Unlock()
	s.stop(j)
	// Only Cloudforge's own log files are removed. The project path is never used.
	if err = j.logs.Remove(); err != nil {
		return fmt.Errorf("o projeto foi parado, mas os logs não puderam ser removidos: %w", err)
	}
	if err = s.store.DeleteProject(id); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()
	deleted = true
	return nil
}
func (s *Supervisor) launch(j *job) {
	j.mu.Lock()
	if j.active {
		j.mu.Unlock()
		return
	}
	j.active = true
	ctx, cancel := context.WithCancel(s.ctx)
	j.cancel = cancel
	j.done = make(chan struct{})
	j.state.ActualState = "starting"
	j.state.LastError = ""
	j.state.Restarts = 0
	j.mu.Unlock()
	go s.loop(ctx, j)
}
func (s *Supervisor) stop(j *job) {
	j.mu.Lock()
	if j.blocked != "" {
		j.mu.Unlock()
		return
	}
	cancel, done, active := j.cancel, j.done, j.active
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if active && done != nil {
		<-done
	}
	j.mu.Lock()
	j.state.ActualState = "offline"
	j.state.PID = 0
	j.state.StartedAt = nil
	j.state.CPU = nil
	j.state.Memory = nil
	j.state.Health = "offline"
	state := j.state
	j.mu.Unlock()
	_ = s.store.Runtime(state)
	j.logs.Add("system", "Aplicação parada. Rotas e túnel preservados.")
}
func (s *Supervisor) loop(ctx context.Context, j *job) {
	defer func() {
		j.mu.Lock()
		j.active = false
		j.tree = nil
		j.cancel = nil
		close(j.done)
		j.mu.Unlock()
	}()
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		j.mu.Lock()
		p := j.p
		j.state.ActualState = "starting"
		j.mu.Unlock()
		var runError error
		if p.Port > 0 {
			listener, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p.Port))
			if e != nil {
				runError = fmt.Errorf("porta %d ocupada: %w", p.Port, e)
			} else {
				listener.Close()
			}
		}
		var cmd *exec.Cmd
		if runError == nil {
			prepareCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
			cmd, runError = runtimes.Prepare(prepareCtx, s.store, p, j.logs.Add)
			cancel()
		}
		// The returned command must use the long-lived execution context, not the preparation deadline.
		if runError == nil {
			fresh := exec.Command(cmd.Path, cmd.Args[1:]...)
			cmd = fresh
			cmd.Dir = p.WorkingDirectory
			cmd.Env = nil
			if p.Port > 0 {
				cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", p.Port))
			}
			var terminal *terminalSession
			if p.TerminalEnabled {
				terminal = newTerminalSession()
				var input io.WriteCloser
				cmd, input, runError = terminalCommand(cmd)
				if runError == nil {
					terminal.input = input
				}
			}
			if runError == nil {
				processes.Configure(cmd)
				stdout := &streamWriter{book: j.logs, stream: "stdout"}
				stderr := &streamWriter{book: j.logs, stream: "stderr"}
				if terminal == nil {
					cmd.Stdout = stdout
				} else {
					cmd.Stdout = io.MultiWriter(stdout, terminal)
				}
				cmd.Stderr = stderr
				cmd.WaitDelay = 2 * time.Second
				if runError == nil && ctx.Err() == nil {
					runError = cmd.Start()
				}
				if runError == nil && cmd.Process != nil {
					tree, e := processes.Attach(cmd)
					if e != nil {
						cmd.Process.Kill()
						cmd.Wait()
						runError = e
					} else {
						now := time.Now().UTC()
						j.mu.Lock()
						j.tree = tree
						j.terminal = terminal
						j.state.PID = cmd.Process.Pid
						if proc, e := process.NewProcess(int32(cmd.Process.Pid)); e == nil {
							j.state.ProcessCreatedAt, _ = proc.CreateTime()
							j.state.Executable, _ = proc.Exe()
						}
						j.state.StartedAt = &now
						j.state.ActualState = "online"
						j.state.LastError = ""
						j.state.Health = "running"
						state := j.state
						j.mu.Unlock()
						if e = s.store.Runtime(state); e != nil {
							tree.Close()
							_ = cmd.Wait()
							j.mu.Lock()
							j.state.ActualState = "error"
							j.state.PID = 0
							j.state.StartedAt = nil
							j.state.LastError = "Não foi possível persistir a execução: " + e.Error()
							j.state.Health = "offline"
							j.mu.Unlock()
							j.logs.Add("system", "Processo encerrado por falha de persistência.")
							return
						}
						j.logs.Add("system", fmt.Sprintf("Aplicação iniciada · PID %d", cmd.Process.Pid))
						wait := make(chan error, 1)
						go func() { result := cmd.Wait(); stdout.Flush(); stderr.Flush(); wait <- result }()
						select {
						case runError = <-wait:
						case <-ctx.Done():
							procDone := make(chan struct{})
							go func() { runError = <-wait; close(procDone) }()
							tree.Stop(procDone)
							<-procDone
						}
						tree.Close()
						j.mu.Lock()
						j.tree = nil
						j.terminal = nil
						j.mu.Unlock()
					}
				}
			}
			if terminal != nil {
				terminal.close()
			}
		}
		if ctx.Err() != nil {
			return
		}
		code := 0
		if runError != nil {
			code = 1
			var exit *exec.ExitError
			if errors.As(runError, &exit) {
				code = exit.ExitCode()
			}
		}
		j.mu.Lock()
		j.state.PID = 0
		j.state.StartedAt = nil
		j.state.LastExitCode = &code
		j.state.CPU = nil
		j.state.Memory = nil
		if len(j.usage) > 0 {
			j.recordUsage()
		}
		j.state.Health = "offline"
		j.state.ActualState = "offline"
		if runError != nil {
			j.state.ActualState = "error"
			j.state.LastError = runError.Error()
		}
		desired := j.p.DesiredState
		policy := j.p.RestartPolicy
		state := j.state
		j.mu.Unlock()
		_ = s.store.Runtime(state)
		j.logs.Add("system", fmt.Sprintf("Processo encerrado · código %d · %v", code, runError))
		if desired != "running" || policy == "never" || (policy == "on-failure" && runError == nil) {
			return
		}
		attempt++
		delay := time.Second * time.Duration(1<<min(attempt, 6))
		j.mu.Lock()
		j.state.Restarts = attempt
		j.state.ActualState = "restarting"
		j.mu.Unlock()
		j.logs.Add("system", fmt.Sprintf("Nova tentativa em %s (tentativa %d).", delay, attempt))
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}
func (s *Supervisor) Views() []model.View {
	s.mu.RLock()
	jobs := make([]*job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	s.mu.RUnlock()
	routes, _ := s.store.Routes()
	out := []model.View{}
	usageSince := time.Now().Add(-usageWindow).UnixMilli()
	for _, j := range jobs {
		j.mu.Lock()
		v := model.View{Project: j.p, RuntimeInfo: j.state}
		first := sort.Search(len(j.usage), func(i int) bool { return j.usage[i].Time >= usageSince })
		v.UsageHistory = slices.Clone(j.usage[first:])
		v.RuntimeInfo.TerminalReady = j.terminal != nil
		j.mu.Unlock()
		for _, r := range routes {
			if r.ProjectID == v.ID {
				v.Hostname = r.Hostname
				v.RoutePort = r.Port
			}
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
func (s *Supervisor) Logs(id string) ([]model.Log, error) {
	j, e := s.get(id)
	if e != nil {
		return nil, e
	}
	return j.logs.Lines(), nil
}
func (s *Supervisor) SetTerminalEnabled(id string, enabled bool) error {
	j, err := s.get(id)
	if err != nil {
		return err
	}
	j.op.Lock()
	defer j.op.Unlock()
	j.mu.Lock()
	project := j.p
	j.mu.Unlock()
	project.TerminalEnabled = enabled
	if err = s.store.SaveProject(project); err != nil {
		return err
	}
	j.mu.Lock()
	j.p = project
	j.mu.Unlock()
	return nil
}
func (s *Supervisor) Online(id string) bool {
	j, e := s.get(id)
	if e != nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state.ActualState == "online"
}
func (s *Supervisor) Shutdown() {
	s.cancel()
	s.mu.RLock()
	jobs := []*job{}
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	s.mu.RUnlock()
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j *job) {
			defer wg.Done()
			j.op.Lock()
			defer j.op.Unlock()
			j.mu.Lock()
			removing := j.removing
			j.mu.Unlock()
			if !removing {
				s.stop(j)
			}
		}(j)
	}
	wg.Wait()
	s.ops.Wait()
}
func (s *Supervisor) observeOperation(j *job, op, id, action string) {
	ctx, cancel := context.WithTimeout(s.ctx, 16*time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	payload := map[string]string{"projectId": id, "action": action}
	finish := func(status, message string) {
		j.mu.Lock()
		defer j.mu.Unlock()
		if !j.removing {
			_ = s.store.Operation(op, "process", status, payload, message)
		}
	}
	for {
		j.mu.Lock()
		state, last := j.state.ActualState, j.state.LastError
		j.mu.Unlock()
		if state == "online" {
			finish("complete", "")
			return
		}
		if last != "" {
			finish("failed", last)
			return
		}
		if state == "offline" {
			finish("cancelled", "execução interrompida")
			return
		}
		select {
		case <-ctx.Done():
			finish("cancelled", "operação interrompida ou expirada")
			return
		case <-ticker.C:
		}
	}
}

const (
	usageWindow   = 2 * time.Minute
	usageInterval = 2 * time.Second
	usageCapacity = int(usageWindow/usageInterval) + 2
)

// recordUsage is called with j.mu held. Samples are immutable after collection.
func (j *job) recordUsage() {
	if len(j.usage) == usageCapacity {
		copy(j.usage, j.usage[1:])
		j.usage = j.usage[:usageCapacity-1]
	}
	j.usage = append(j.usage, model.UsageSample{
		Time: time.Now().UnixMilli(), CPU: j.state.CPU, Memory: j.state.Memory,
	})
}

func (s *Supervisor) monitor() {
	cache := map[int32]*process.Process{}
	ticker := time.NewTicker(usageInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.RLock()
		jobs := []*job{}
		for _, j := range s.jobs {
			jobs = append(jobs, j)
		}
		s.mu.RUnlock()
		for _, j := range jobs {
			j.mu.Lock()
			pid := j.state.PID
			startedAt := j.state.StartedAt
			j.mu.Unlock()
			if pid == 0 {
				continue
			}
			proc, e := process.NewProcess(int32(pid))
			if e != nil {
				continue
			}
			children, _ := proc.Children()
			all := []*process.Process{proc}
			for i := 0; i < len(children) && i < 1024; i++ {
				all = append(all, children[i])
				more, _ := children[i].Children()
				children = append(children, more...)
			}
			var cpu float64
			var memory uint64
			okCPU, okRAM := true, true
			for _, p := range all {
				if cached, exists := cache[p.Pid]; exists {
					p = cached
				} else {
					cache[p.Pid] = p
				}
				v, e := p.Percent(0)
				if e != nil {
					okCPU = false
				} else {
					cpu += v
				}
				m, e := p.MemoryInfo()
				if e != nil {
					okRAM = false
				} else {
					memory += m.RSS
				}
			}
			j.mu.Lock()
			if j.state.PID == pid && j.state.StartedAt == startedAt && j.state.ActualState == "online" {
				j.state.CPU = nil
				j.state.Memory = nil
				if okCPU {
					j.state.CPU = &cpu
				}
				if okRAM {
					j.state.Memory = &memory
				}
				j.recordUsage()
				j.state.Children = len(all) - 1
				if j.p.Port > 0 {
					conn, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", j.p.Port), 100*time.Millisecond)
					if e == nil {
						conn.Close()
						j.state.Health = "http-listening"
					} else {
						j.state.Health = "port-unavailable"
					}
				}
			}
			j.mu.Unlock()
		}
	}
}
