package databases

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

func dockerBytes(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		factor float64
	}{
		{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"B", 1},
	}
	for _, unit := range units {
		if number, ok := strings.CutSuffix(s, unit.suffix); ok {
			n, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
			if err != nil || n < 0 {
				return 0, errors.New("métrica do Docker inválida")
			}
			return uint64(n * unit.factor), nil
		}
	}
	return 0, errors.New("unidade do Docker inválida")
}

func (d docker) metrics(ctx context.Context, r record) (Metric, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := d.command(ctx, nil, "stats", "--no-stream", "--format", "{{json .}}", containerName(r))
	if err != nil {
		return Metric{}, err
	}
	var raw struct{ CPUPerc, MemUsage, NetIO string }
	if err = json.Unmarshal(out, &raw); err != nil {
		return Metric{}, err
	}
	var sample Metric
	sample.Time = time.Now().UnixMilli()
	sample.CPU, err = strconv.ParseFloat(strings.TrimSuffix(raw.CPUPerc, "%"), 64)
	if err != nil {
		return Metric{}, err
	}
	mem, limit, _ := strings.Cut(raw.MemUsage, "/")
	sample.Memory, err = dockerBytes(mem)
	if err != nil {
		return Metric{}, err
	}
	sample.MemoryLimit, err = dockerBytes(limit)
	if err != nil {
		return Metric{}, err
	}
	rx, tx, _ := strings.Cut(raw.NetIO, "/")
	sample.RX, err = dockerBytes(rx)
	if err != nil {
		return Metric{}, err
	}
	sample.TX, err = dockerBytes(tx)
	if err != nil {
		return Metric{}, err
	}
	out, err = d.run(ctx, r, "du", nil, "-sk", engines[r.Engine].Data)
	if fields := strings.Fields(string(out)); err == nil && len(fields) > 0 {
		if n, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			n *= 1024
			sample.Storage = &n
		}
	}
	return sample, nil
}
