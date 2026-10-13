// Package snowflake creates node-scoped, time-sortable legacy bigint IDs.
package snowflake

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"yaginx-gateway/internal/platform/atomicfile"
)

const Epoch = int64(1767225600000) // 2026-01-01T00:00:00Z in milliseconds.

var ErrClockRollback = errors.New("snowflake clock moved backwards by more than 5ms")

type persisted struct {
	LastMillis int64 `json:"lastMillis"`
}

type Generator struct {
	mu         sync.Mutex
	node       int64
	lastMillis int64
	sequence   int64
	now        func() time.Time
	statePath  string
}

func LoadOrCreate(node int, statePath string) (*Generator, error) {
	if node < 0 || node > 1023 {
		return nil, fmt.Errorf("snowflake node number must be 0..1023")
	}
	g := &Generator{node: int64(node), now: time.Now, statePath: statePath, sequence: -1}
	if statePath == "" {
		return g, nil
	}
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return g, nil
	}
	if err != nil {
		return nil, err
	}
	var value persisted
	if err := json.Unmarshal(data, &value); err != nil || value.LastMillis < 0 {
		if err == nil {
			err = fmt.Errorf("lastMillis is invalid")
		}
		return nil, fmt.Errorf("decode snowflake state: %w", err)
	}
	g.lastMillis = value.LastMillis
	return g, nil
}

func (g *Generator) Next() (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for {
		millis := g.now().UTC().UnixMilli() - Epoch
		if millis < 0 {
			return 0, fmt.Errorf("snowflake clock precedes epoch")
		}
		if millis < g.lastMillis {
			if g.lastMillis-millis > 5 {
				return 0, ErrClockRollback
			}
			millis = g.lastMillis
		}
		if millis == g.lastMillis {
			g.sequence++
			if g.sequence > 4095 {
				time.Sleep(time.Millisecond)
				continue
			}
		} else {
			g.lastMillis, g.sequence = millis, 0
		}
		return (g.lastMillis << 22) | (g.node << 12) | g.sequence, nil
	}
}

func (g *Generator) Save() error {
	g.mu.Lock()
	value := persisted{LastMillis: g.lastMillis}
	g.mu.Unlock()
	if g.statePath == "" {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return atomicfile.Write(g.statePath, data, 0o600)
}

func (g *Generator) RunPersistence(ctxDone <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctxDone:
			_ = g.Save()
			return
		case <-ticker.C:
			_ = g.Save()
		}
	}
}
