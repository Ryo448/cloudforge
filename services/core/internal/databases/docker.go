package databases

import (
	"bytes"
	"cloudforge/core/internal/processes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type commandError struct{ operation string }

func (e commandError) Error() string { return "Docker: falha em " + e.operation }

var errPortInUse = errors.New("a porta local reservada para o banco está ocupada")

type docker struct{}

func (docker) command(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	socket := "unix:///var/run/docker.sock"
	if runtime.GOOS == "windows" {
		socket = "npipe:////./pipe/docker_engine"
	} else if dir := os.Getenv("XDG_RUNTIME_DIR"); strings.HasPrefix(dir, "/") {
		if _, err := os.Stat(dir + "/docker.sock"); err == nil {
			socket = "unix://" + dir + "/docker.sock"
		}
	}
	cmd := exec.CommandContext(ctx, "docker", append([]string{"--host", socket}, args...)...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "DOCKER_CONTEXT", "DOCKER_HOST", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH":
		default:
			cmd.Env = append(cmd.Env, entry)
		}
	}
	processes.ConfigureBackground(cmd)
	cmd.Stdin = input
	var output bytes.Buffer
	cmd.Stdout = &output
	// Driver output can include connection strings or SQL with credentials.
	var diagnostic bytes.Buffer
	if args[0] == "start" {
		cmd.Stderr = &diagnostic
	} else {
		cmd.Stderr = io.Discard
	}
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		message := strings.ToLower(diagnostic.String())
		if strings.Contains(message, "address already in use") || strings.Contains(message, "port is already allocated") || strings.Contains(message, "ports are not available") {
			return nil, errPortInUse
		}
		return nil, commandError{args[0]}
	}
	return output.Bytes(), nil
}

func containerName(r record) string { return "cloudforge-db-" + r.ID }

func (d docker) available(ctx context.Context) bool {
	out, err := d.command(ctx, nil, "version", "--format", "{{.Server.Os}} {{.Server.Version}}")
	fields := strings.Fields(string(out))
	if err != nil || len(fields) != 2 || fields[0] != "linux" {
		return false
	}
	major, _, _ := strings.Cut(fields[1], ".")
	version, err := strconv.Atoi(major)
	// Older engines allowed peers on the same L2 network to reach loopback ports.
	return err == nil && version >= 28
}

type portBinding struct{ HostIP, HostPort string }

type attachedNetwork struct {
	NetworkID string
	Aliases   []string
}

type containerState struct {
	Running   bool
	StartedAt time.Time
	LocalPort int
	Local     bool
	TLS       bool
	Image     string
	Networks  map[string]attachedNetwork
}

func (d docker) inspect(ctx context.Context, r record) (containerState, bool, error) {
	return d.inspectNamed(ctx, r, containerName(r))
}

func (d docker) inspectNamed(ctx context.Context, r record, name string) (containerState, bool, error) {
	var state containerState
	out, err := d.command(ctx, nil, "ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return state, false, err
	}
	if strings.TrimSpace(string(out)) != name {
		return state, false, nil
	}
	format := `{{json .State}} {{json .Image}} {{json .Config.Labels}} {{json .HostConfig.PortBindings}} {{json .HostConfig.NetworkMode}} {{json .NetworkSettings.Ports}} {{json .NetworkSettings.Networks}}`
	raw, err := d.command(ctx, nil, "inspect", "--type", "container", "--format", format, name)
	if err != nil {
		return state, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var mode string
	var labels map[string]string
	var configured, active map[string][]portBinding
	for _, dst := range []any{&state, &state.Image, &labels, &configured, &mode, &active, &state.Networks} {
		if err = decoder.Decode(dst); err != nil {
			return state, false, errors.New("configuração do container inválida")
		}
	}
	if labels["cloudforge.database"] != r.ID {
		return state, false, errors.New("container encontrado sem identificação válida")
	}
	key := fmt.Sprintf("%d/tcp", engines[r.Engine].Port)
	state.LocalPort = loopbackPort(active, key)
	configuredPort := loopbackPort(configured, key)
	state.Local = labels["cloudforge.database.local"] == "1" && mode != "host" && configuredPort != 0 &&
		(!state.Running || state.LocalPort == configuredPort)
	state.TLS = labels["cloudforge.database.tls"] == "1"
	return state, true, nil
}

func loopbackPort(bindings map[string][]portBinding, key string) int {
	for port, entries := range bindings {
		if port != key && len(entries) > 0 {
			return 0
		}
	}
	entries := bindings[key]
	if len(entries) != 1 || entries[0].HostIP != localHost {
		return 0
	}
	port, err := strconv.Atoi(entries[0].HostPort)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

const waitForConfig = `trap 'exit 0' TERM INT

while [ ! -f /run/cloudforge/ready ]; do sleep 1; done

`

func boot(engine string) []string {
	switch engine {
	case "postgresql":
		return []string{"POSTGRES_USER=cfadmin", "POSTGRES_PASSWORD_FILE=/run/cloudforge/admin", "POSTGRES_DB=cloudforge", "POSTGRES_HOST_AUTH_METHOD=scram-sha-256",
			waitForConfig + "exec docker-entrypoint.sh postgres -c hba_file=/run/cloudforge/pg_hba.conf -c ssl=on -c ssl_cert_file=/run/cloudforge/server.pem -c ssl_key_file=/run/cloudforge/server.key -c ssl_ca_file=/run/cloudforge/ca.crt"}
	case "mysql":
		return []string{"MYSQL_ROOT_PASSWORD_FILE=/run/cloudforge/admin", "MYSQL_ROOT_HOST=localhost", "MYSQL_DATABASE=cloudforge",
			waitForConfig + "exec docker-entrypoint.sh mysqld --mysqlx=OFF --require-secure-transport=ON --ssl-ca=/run/cloudforge/ca.crt --ssl-cert=/run/cloudforge/server.pem --ssl-key=/run/cloudforge/server.key"}
	case "mongodb":
		return []string{"MONGO_INITDB_ROOT_USERNAME=cfadmin", "MONGO_INITDB_ROOT_PASSWORD_FILE=/run/cloudforge/admin",
			waitForConfig + "exec docker-entrypoint.sh mongod --bind_ip_all --tlsMode requireTLS --tlsCertificateKeyFile /run/cloudforge/mongo.pem --tlsCAFile /run/cloudforge/ca.crt --tlsAllowConnectionsWithoutCertificates"}
	default:
		return []string{waitForConfig + "exec gosu redis redis-server --port 0 --tls-port 6379 --tls-auth-clients optional --tls-ca-cert-file /run/cloudforge/ca.crt --tls-cert-file /run/cloudforge/server.pem --tls-key-file /run/cloudforge/server.key --aclfile /run/cloudforge/users.acl --appendonly yes --maxmemory 768mb --maxmemory-policy noeviction"}
	}
}

func (d docker) configure(ctx context.Context, r record, s secret, material *serverTLS) error {
	const configure = `umask 077

cd /run/cloudforge

[ ! -f ready ] || exit 0

read_pem() {
	while IFS= read -r line; do
		[ "$line" != "." ] || return 0
		printf '%s\n' "$line"
	done
	return 1
}

read_pem > ca.crt
read_pem > server.pem
read_pem > server.key
chmod 644 ca.crt server.pem
chmod 600 server.key

if [ "$1" = redis ]; then

	cat > users.acl

	chown redis:redis users.acl server.key

else

	cat > admin

	case "$1" in

	postgresql)

		cat > pg_hba.conf <<'HBA'

local all cfadmin scram-sha-256

hostssl all cfadmin 127.0.0.1/32 scram-sha-256

hostssl all cfadmin ::1/128 scram-sha-256

hostssl cloudforge cloudforge all scram-sha-256

HBA

		chown postgres:postgres admin pg_hba.conf server.key ;;

	mysql) chown mysql:mysql admin server.key ;;

	mongodb)
		cat server.pem server.key > mongo.pem
		chown mongodb:mongodb admin mongo.pem
		rm server.key ;;

	esac

fi

touch ready`
	content := s.Admin
	if r.Engine == "redis" {
		content = fmt.Sprintf(`user default off

user cfadmin on #%x ~* &* +@all

user cloudforge on #%x ~* &* +@all -@admin -@dangerous

`,
			passwordHash(s.Admin), passwordHash(s.Password))
	}
	input := io.MultiReader(bytes.NewReader(material.CA), strings.NewReader(".\n"),
		bytes.NewReader(material.PEM), strings.NewReader(".\n"), bytes.NewReader(material.Key),
		strings.NewReader(".\n"+content))
	_, err := d.run(ctx, r, "/bin/sh", input, "-ec", configure, "configure", r.Engine)
	return err
}

func (d docker) create(ctx context.Context, r record) error {
	e := engines[r.Engine]
	name := containerName(r)
	legacy, hasLegacy, err := d.inspectNamed(ctx, r, name+"-legacy")
	if err != nil {
		return err
	}
	image := e.Image
	if hasLegacy {
		image = legacy.Image
	}
	if _, err = d.command(ctx, nil, "image", "inspect", "--format", "{{.Id}}", image); err != nil {
		return fmt.Errorf("imagem Docker indisponível localmente: %s", image)
	}
	for _, kind := range []string{"volume", "network"} {
		out, err := d.command(ctx, nil, kind, "ls", "--filter", "name=^"+name+"$", "--format", "{{.Name}}")
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(out)) == "" {
			if hasLegacy && kind == "volume" {
				return errors.New("volume original do banco indisponível")
			}
			_, _ = d.command(ctx, nil, kind, "create", "--label", "cloudforge.database="+r.ID, name)
		}
		out, err = d.command(ctx, nil, kind, "inspect", name, "--format", `{{ index .Labels "cloudforge.database" }}`)
		if err != nil || strings.TrimSpace(string(out)) != r.ID {
			return errors.New("recurso Docker encontrado sem identificação válida: " + kind)
		}
	}
	args := []string{"create", "--pull", "never", "--name", name, "--label", "cloudforge.database=" + r.ID,
		"--label", "cloudforge.database.local=1", "--label", "cloudforge.database.tls=1", "--network", name,
		"--publish", fmt.Sprintf("%s:%d:%d", localHost, r.ReservedPort, e.Port),
		"--mount", "type=volume,source=" + name + ",target=" + e.Data, "--tmpfs", "/run/cloudforge:rw,noexec,nosuid,nodev,mode=0755",
		"--cpus", "1", "--pids-limit", "256",
		"--security-opt", "no-new-privileges", "--log-opt", "max-size=10m", "--log-opt", "max-file=2"}
	memoryMB := defaultMemoryMB
	if r.MemoryMB != nil {
		memoryMB = *r.MemoryMB
	}
	if memoryMB > 0 {
		limit := strconv.Itoa(memoryMB) + "m"
		args = append(args, "--memory", limit, "--memory-swap", limit)
	}
	envAndScript := boot(r.Engine)
	for _, entry := range envAndScript[:len(envAndScript)-1] {
		args = append(args, "--env", entry)
	}
	args = append(args, "--entrypoint", "/bin/sh", image, "-ec", envAndScript[len(envAndScript)-1])
	_, err = d.command(ctx, nil, args...)
	return err
}

func (d docker) stageLegacy(ctx context.Context, r record, state containerState) error {
	if state.Running {
		if err := d.lifecycle(ctx, r, "stop", "--time", "15"); err != nil {
			return err
		}
	}
	name := containerName(r)
	_, err := d.command(ctx, nil, "rename", name, name+"-legacy")
	return err
}

func (d docker) restoreNetworks(ctx context.Context, r record, state containerState) error {
	name := containerName(r)
	legacy, exists, err := d.inspectNamed(ctx, r, name+"-legacy")
	if err != nil || !exists {
		return err
	}
	for network, previous := range legacy.Networks {
		if network == name || previous.NetworkID == "" || state.Networks[network].NetworkID == previous.NetworkID {
			continue
		}
		args := []string{"network", "connect"}
		if network != "bridge" {
			for _, alias := range previous.Aliases {
				if alias != name {
					args = append(args, "--alias", alias)
				}
			}
		}
		if _, err = d.command(ctx, nil, append(args, previous.NetworkID, name)...); err != nil {
			return err
		}
	}
	return nil
}

func (d docker) removeLegacy(ctx context.Context, r record) error {
	name := containerName(r) + "-legacy"
	_, exists, err := d.inspectNamed(ctx, r, name)
	if err != nil || !exists {
		return err
	}
	_, err = d.command(ctx, nil, "rm", "-v", name)
	return err
}

func (d docker) run(ctx context.Context, r record, cmd string, input io.Reader, args ...string) ([]byte, error) {
	prefix := []string{"exec", "-i", containerName(r), cmd}
	return d.command(ctx, input, append(prefix, args...)...)
}

func (d docker) lifecycle(ctx context.Context, r record, args ...string) error {
	_, err := d.command(ctx, nil, append(args, containerName(r))...)
	return err
}

func (d docker) remove(ctx context.Context, r record) error {
	_, exists, err := d.inspect(ctx, r)
	if err != nil {
		return err
	}
	if exists {
		if err = d.lifecycle(ctx, r, "rm", "-f", "-v"); err != nil {
			return err
		}
	}
	if err = d.removeLegacy(ctx, r); err != nil {
		return err
	}
	name := containerName(r)
	for _, kind := range []string{"network", "volume"} {
		out, err := d.command(ctx, nil, kind, "ls", "--filter", "label=cloudforge.database="+r.ID, "--format", "{{.Name}}")
		if err != nil {
			return err
		}
		for _, line := range strings.Fields(string(out)) {
			if line == name {
				if _, err = d.command(ctx, nil, kind, "rm", name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
