package model

import "time"

type Project struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Runtime          string    `json:"runtime"`
	Path             string    `json:"path"`
	EntryPoint       string    `json:"entryPoint"`
	StartCommand     []string  `json:"startCommand"`
	Arguments        []string  `json:"arguments"`
	WorkingDirectory string    `json:"workingDirectory"`
	DesiredState     string    `json:"desiredState"`
	RestartPolicy    string    `json:"restartPolicy"`
	TerminalEnabled  bool      `json:"terminalEnabled"`
	Port             int       `json:"port"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
type Runtime struct {
	ProcessCreatedAt int64      `json:"processCreatedAt,omitempty"`
	Executable       string     `json:"executable,omitempty"`
	ProjectID        string     `json:"projectId"`
	PID              int        `json:"pid"`
	ActualState      string     `json:"actualState"`
	StartedAt        *time.Time `json:"startedAt"`
	LastExitCode     *int       `json:"lastExitCode"`
	LastError        string     `json:"lastError"`
	CPU              *float64   `json:"cpu"`
	Memory           *uint64    `json:"memory"`
	Children         int        `json:"children"`
	Restarts         int        `json:"restarts"`
	Health           string     `json:"health"`
	TerminalReady    bool       `json:"terminalReady,omitempty"`
}
type UsageSample struct {
	Time   int64    `json:"time"`
	CPU    *float64 `json:"cpu"`
	Memory *uint64  `json:"memory"`
}
type View struct {
	Project
	RuntimeInfo  Runtime       `json:"execution"`
	UsageHistory []UsageSample `json:"usageHistory,omitempty"`
	Hostname     string        `json:"hostname,omitempty"`
	RoutePort    int           `json:"routePort,omitempty"`
}
type Dependencies struct {
	EnvironmentFingerprint string    `json:"environmentFingerprint"`
	ProjectID              string    `json:"projectId"`
	ManifestHash           string    `json:"manifestHash"`
	LockfileHash           string    `json:"lockfileHash"`
	RuntimeVersion         string    `json:"runtimeVersion"`
	PackageManager         string    `json:"packageManager"`
	LastSuccessfulInstall  time.Time `json:"lastSuccessfulInstall"`
	InstallStatus          string    `json:"installStatus"`
}
type Route struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	ZoneID      string `json:"zoneId"`
	Hostname    string `json:"hostname"`
	Port        int    `json:"port"`
	DNSRecordID string `json:"dnsRecordId"`
}
type CloudConfig struct {
	Mode      string `json:"mode,omitempty"`
	AccountID string `json:"accountId"`
	ZoneID    string `json:"zoneId"`
	Domain    string `json:"domain"`
	AdminHost string `json:"adminHost"`
	TunnelID  string `json:"tunnelId"`
	Name      string `json:"name"`
}
type Log struct {
	Seq     uint64    `json:"seq"`
	Time    time.Time `json:"time"`
	Stream  string    `json:"stream"`
	Message string    `json:"message"`
}
