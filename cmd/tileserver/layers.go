package main

import (
	"io"
	"log/slog"
	"sync"

	"github.com/kdudkov/tileproxy/pkg/model"
)

func NewLayers() *Layers { return &Layers{data: make(map[string]model.Source)} }

type Layers struct {
	mu   sync.RWMutex
	data map[string]model.Source
}

func closeSource(source model.Source) {
	if closer, ok := source.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			slog.Error("close layer", "layer", source.GetKey(), "error", err)
		}
	}
}

func (h *Layers) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, source := range h.data {
		closeSource(source)
		delete(h.data, key)
	}
}

// Acquire pins the source until release is called, so reload cannot close an active reader.
// ponytail: a shared read lock keeps reload simple; per-layer leases if slow requests delay reloads.
func (h *Layers) Acquire(key string) (model.Source, func()) {
	h.mu.RLock()
	return h.data[key], h.mu.RUnlock
}

func (h *Layers) Add(source model.Source) {
	if source == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.data[source.GetKey()]; old != nil && old != source {
		closeSource(old)
	}
	h.data[source.GetKey()] = source
}

func (h *Layers) Remove(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	closeSource(h.data[key])
	delete(h.data, key)
}

// ReplaceFiles publishes the new set together, after active readers release the old set.
func (h *Layers) ReplaceFiles(sources []model.Source) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, source := range h.data {
		if source.IsFile() {
			closeSource(source)
			delete(h.data, key)
		}
	}
	for _, source := range sources {
		if old := h.data[source.GetKey()]; old != nil && old != source {
			closeSource(old)
		}
		h.data[source.GetKey()] = source
	}
}

func (h *Layers) All(f func(model.Source) bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, source := range h.data {
		if !f(source) {
			return
		}
	}
}
