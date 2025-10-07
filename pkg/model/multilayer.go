package model

import "context"

var _ Source = &MultiLayer{}

type MultiLayer struct {
	key     string
	name    string
	minZoom int
	maxZoom int
	layers  []*Layer
}

func NewMultilayer(key, name string, layers []*Layer) *MultiLayer {
	m := &MultiLayer{
		key:    key,
		name:   name,
		layers: layers,
	}
	
	m.init()
	
	return m
}

func (m *MultiLayer) init() {
	if len(m.layers) == 0 {
		panic("no layers")
	}
	
	m.minZoom = m.layers[0].GetMinZoom()
	m.maxZoom = m.layers[0].GetMaxZoom()
	
	for _, l := range m.layers {
		m.minZoom = min(m.minZoom, l.GetMinZoom())
		m.maxZoom = max(m.maxZoom, l.GetMaxZoom())
	}
}

func (m *MultiLayer) GetKey() string {
	return m.key
}

func (m *MultiLayer) GetMaxZoom() int {
	return m.maxZoom
}

func (m *MultiLayer) GetMinZoom() int {
	return m.minZoom
}

func (m *MultiLayer) GetName() string {
	return m.name
}

func (m *MultiLayer) IsFile() bool {
	return true
}

func (m *MultiLayer) IsTms() bool {
	return false
}

func (m *MultiLayer) GetTile(ctx context.Context, z int, x int, y int) (string, []byte, error) {
	for _, l := range m.layers {
		ct, b, err := l.GetTile(ctx, z, x, y)

		if err != nil || len(b) > 0 {
			return ct, b, err
		}
	}

	return "", nil, nil
}
