package databases

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const redisCLI = "redis-cli --tls --cacert /run/cloudforge/ca.crt -h 127.0.0.1 -p 6379 --user cfadmin"

func (d *SnapshotDatabase) dumpRedis(input io.Reader, out io.Writer) error {
	return d.exec(input, out, "/bin/sh", "-ec", redisAdmin+
		"d=$(mktemp -d); trap 'rm -rf -- \"$d\"' EXIT; "+
		redisCLI+" --rdb \"$d/snapshot.rdb\" >&2; redis-check-rdb \"$d/snapshot.rdb\" >&2; cat \"$d/snapshot.rdb\"")
}

const redisStage = `
IFS= read -r ignored
umask 077
mkdir "$1"
cat > "$1/snapshot.rdb"
redis-check-rdb "$1/snapshot.rdb" >/dev/null
redis-server --bind 127.0.0.1 --port "$2" --dir "$1" --dbfilename snapshot.rdb \
  --save "" --appendonly no --daemonize yes --pidfile "$1/redis.pid" --logfile "$1/redis.log"
`

func (d *SnapshotDatabase) restoreRedis(input io.Reader) error {
	random, err := randomHex(8)
	if err != nil {
		return err
	}
	portNumber, _ := strconv.ParseUint(random[:4], 16, 16)
	port := strconv.FormatUint(20000+portNumber%40000, 10)
	dir := "/tmp/cloudforge-restore-" + random
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cleanup := *d
		cleanup.ctx = ctx
		_ = cleanup.exec(nil, io.Discard, "/bin/sh", "-ec",
			"if [ -s \"$1/redis.pid\" ]; then redis-cli -p \"$2\" shutdown nosave >/dev/null 2>&1 || true; fi; rm -rf -- \"$1\"",
			"cleanup", dir, port)
	}()
	if err := d.exec(input, io.Discard, "/bin/sh", "-ec", redisStage, "stage", dir, port); err != nil {
		return errors.New("o RDB não pôde ser validado ou carregado pela versão instalada do Redis")
	}
	const bridge = `
for i in {1..30}; do
  if { exec 3<>"/dev/tcp/127.0.0.1/$1"; } 2>/dev/null; then
    cat <&3 & reader=$!
    trap 'kill "$reader" 2>/dev/null || true' EXIT
    cat >&3
    exit 0
  fi
  sleep .1
done
exit 1
`
	bridgeIn, toStage := io.Pipe()
	fromStage, bridgeOut := io.Pipe()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer bridgeIn.Close()
		err := d.exec(bridgeIn, bridgeOut, "/bin/bash", "-c", bridge, "bridge", port)
		_ = bridgeOut.CloseWithError(err)
	}()
	defer func() {
		_ = toStage.Close()
		_ = fromStage.Close()
		<-finished
	}()
	stage := redisWire{toStage, bufio.NewReader(fromStage)}
	state, exists, err := (docker{}).inspect(d.ctx, d.record)
	if err != nil || !exists || !state.Running {
		return errors.New("Redis de destino indisponível")
	}
	conn, err := dialTLS(d.ctx, d.record, d.secret.TLS, state.LocalPort)
	if err != nil {
		return errors.New("não foi possível conectar ao Redis de destino")
	}
	defer conn.Close()
	if deadline, ok := d.ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(d.ctx, func() { _ = conn.Close() })
	defer stop()
	target := redisWire{conn, bufio.NewReader(conn)}
	if _, err := target.call("AUTH", "cfadmin", d.secret.Admin); err != nil {
		return errors.New("autenticação da restauração Redis falhou")
	}
	if err := copyRedisSnapshot(stage, target); err != nil {
		return fmt.Errorf("a restauração Redis foi interrompida: %w", err)
	}
	return nil
}

func copyRedisSnapshot(source, target redisWire) error {
	for db := 0; db < 16; db++ {
		index := strconv.Itoa(db)
		if _, err := source.call("SELECT", index); err != nil {
			return err
		}
		if _, err := target.call("SELECT", index); err != nil {
			return err
		}
		cursor := "0"
		for {
			rows, err := source.call("SCAN", cursor, "COUNT", "256")
			if err != nil {
				return err
			}
			result, ok := rows.([]any)
			if !ok || len(result) != 2 {
				return errRedisReply
			}
			cursor, ok = result[0].(string)
			keys, keysOK := result[1].([]any)
			if !ok || !keysOK {
				return errRedisReply
			}
			for _, item := range keys {
				key, ok := item.(string)
				if !ok {
					return errRedisReply
				}
				raw, err := source.call("DUMP", key)
				if err != nil {
					return err
				}
				payload, ok := raw.(string)
				if !ok {
					continue
				}
				ttl, err := source.call("PEXPIRETIME", key)
				if err != nil {
					return err
				}
				expires, ok := ttl.(int64)
				if !ok {
					return errRedisReply
				}
				switch expires {
				case -2:
					continue
				case -1:
					expires = 0
				}
				if _, err := target.call("RESTORE", key, strconv.FormatInt(expires, 10), payload, "REPLACE", "ABSTTL"); err != nil {
					return err
				}
			}
			if cursor == "0" {
				break
			}
		}
	}
	return nil
}

var errRedisReply = errors.New("resposta inválida do Redis")

type redisWire struct {
	out io.Writer
	in  *bufio.Reader
}

func (w redisWire) call(args ...string) (any, error) {
	if _, err := fmt.Fprintf(w.out, "*%d\r\n", len(args)); err != nil {
		return nil, err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(w.out, "$%d\r\n", len(arg)); err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w.out, arg); err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w.out, "\r\n"); err != nil {
			return nil, err
		}
	}
	return w.read(0)
}

func (w redisWire) read(depth int) (any, error) {
	head, err := w.in.ReadSlice('\n')
	if err != nil || len(head) < 3 || head[len(head)-2] != '\r' || depth > 3 {
		return nil, errRedisReply
	}
	body := string(head[1 : len(head)-2])
	switch head[0] {
	case '+':
		return body, nil
	case '-':
		if strings.HasPrefix(body, "OOM ") {
			return nil, errors.New("memória insuficiente no Redis de destino")
		}
		return nil, errors.New("o Redis recusou um comando de restauração")
	case ':':
		return strconv.ParseInt(body, 10, 64)
	case '$', '*':
		n, err := strconv.Atoi(body)
		if err != nil || n < -1 || n > 512*1024*1024 {
			return nil, errRedisReply
		}
		if n == -1 {
			return nil, nil
		}
		if head[0] == '$' {
			buf := make([]byte, n+2)
			if _, err := io.ReadFull(w.in, buf); err != nil || string(buf[n:]) != "\r\n" {
				return nil, errRedisReply
			}
			return string(buf[:n]), nil
		}
		if n > 65536 {
			return nil, errRedisReply
		}
		values := make([]any, n)
		for i := range values {
			values[i], err = w.read(depth + 1)
			if err != nil {
				return nil, err
			}
		}
		return values, nil
	}
	return nil, errRedisReply
}
