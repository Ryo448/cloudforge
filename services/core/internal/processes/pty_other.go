//go:build !windows

package processes

import "errors"

func RunPTYChild() error { return errors.New("terminal interativo disponível somente no Windows") }
