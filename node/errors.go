package node

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrDatadirUsed    = errors.New("datadir already used by another process")
	ErrNodeStopped    = errors.New("node not started")
	ErrNodeRunning    = errors.New("node already running")
	ErrServiceUnknown = errors.New("unknown service")

	datadirInUseErrnos = map[uint]bool{11: true, 32: true, 35: true}
)

type DuplicateServiceError struct {
	Kind reflect.Type
}

func (e *DuplicateServiceError) Error() string {
	return fmt.Sprintf("duplicate service: %v", e.Kind)
}
