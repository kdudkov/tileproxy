package main

import (
	"sync"
	"github.com/kdudkov/tileproxy/pkg/model"
)

func NewLayers() *Layers {
	return &Layers{
		data: sync.Map{},
	}
}

type Layers struct {
	data sync.Map
}

func (h *Layers) Clear() {
	h.data.Clear()
}

func (h *Layers) Get(key string) (model.Source, bool) {
	if v, ok := h.data.Load(key); ok {
		if n, ok1 := v.(model.Source); ok1 {
			return n, true
		}
	}

	return nil, false
}

func (h *Layers) Add(c model.Source) {
	if c == nil {
		return
	}

	h.data.Store(c.GetKey(), c)
}

func (h *Layers) Remove(key string) {
	h.data.Delete(key)
}

func (h *Layers) RemoveFiles() {
	h.All(func(c model.Source) bool {
		if c.IsFile() {
			h.data.Delete(c.GetKey())
		}
		
		return true
	})
}

func (h *Layers) All(f func(c model.Source) bool) {
	h.data.Range(func(_, value any) bool {
		if c, ok := value.(model.Source); ok {
			return f(c)
		}

		return true
	})
}