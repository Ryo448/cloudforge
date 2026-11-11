package monitoring

import (
	"context"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"
)

type System struct {
	Hostname string                 `json:"hostname"`
	OS       string                 `json:"os"`
	Arch     string                 `json:"arch"`
	CPUModel string                 `json:"cpuModel"`
	Cores    int                    `json:"cores"`
	Threads  int                    `json:"threads"`
	CPU      *float64               `json:"cpu"`
	Memory   *mem.VirtualMemoryStat `json:"memory"`
	Disk     *disk.UsageStat        `json:"disk"`
	Uptime   uint64                 `json:"uptime"`
	Runtimes map[string]string      `json:"runtimes"`
	Internet bool                   `json:"internet"`
	Errors   []string               `json:"errors"`
}

func Inspect(path string) System {
	s := System{OS: runtime.GOOS, Arch: runtime.GOARCH, Runtimes: map[string]string{}, Errors: []string{}}
	s.Hostname, _ = os.Hostname()
	info, e := cpu.Info()
	if e == nil && len(info) > 0 {
		s.CPUModel = info[0].ModelName
	}
	s.Cores, _ = cpu.Counts(false)
	s.Threads, _ = cpu.Counts(true)
	if p, e := cpu.Percent(0, false); e == nil && len(p) > 0 {
		s.CPU = &p[0]
	}
	s.Memory, e = mem.VirtualMemory()
	if e != nil {
		s.Errors = append(s.Errors, e.Error())
	}
	s.Disk, e = disk.Usage(path)
	if e != nil {
		s.Errors = append(s.Errors, e.Error())
	}
	s.Uptime, _ = host.Uptime()
	for _, tool := range []string{"python", "python3", "node", "npm", "cloudflared"} {
		if executable, e := exec.LookPath(tool); e == nil {
			s.Runtimes[tool] = executable
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_, e = net.DefaultResolver.LookupHost(ctx, "api.cloudflare.com")
	s.Internet = e == nil
	return s
}
