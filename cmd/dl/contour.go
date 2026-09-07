package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

type point struct{ lon, lat float64 }
type contour []point
type tile struct{ z, x, y int }
type zoomPlan struct {
	z, xmin, xmax, ymin, ymax int
	count                     int64
}

const epsilon = 1e-12
const mercatorLimit = 85.0511287798066

func readContour(filename string) (contour, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var geometry struct {
		Type        string         `json:"type"`
		Coordinates [][][]*float64 `json:"coordinates"`
	}
	if err := json.Unmarshal(data, &geometry); err != nil {
		return nil, fmt.Errorf("invalid GeoJSON: %w", err)
	}
	if geometry.Type != "Polygon" || len(geometry.Coordinates) != 1 {
		return nil, fmt.Errorf("expected a GeoJSON Polygon with one outer ring and no holes")
	}
	ring := geometry.Coordinates[0]
	if len(ring) < 4 {
		return nil, fmt.Errorf("contour needs at least three vertices and a closing coordinate")
	}
	p := make(contour, len(ring))
	for i, c := range ring {
		if len(c) != 2 || c[0] == nil || c[1] == nil || math.IsNaN(*c[0]) || math.IsNaN(*c[1]) || math.IsInf(*c[0], 0) || math.IsInf(*c[1], 0) || math.Abs(*c[0]) > 180 || math.Abs(*c[1]) > mercatorLimit {
			return nil, fmt.Errorf("invalid coordinate %d: expected longitude [-180,180], latitude within Web Mercator bounds", i)
		}
		p[i] = point{*c[0], *c[1]}
	}
	if p[0] != p[len(p)-1] {
		return nil, fmt.Errorf("contour ring is not closed")
	}
	p = p[:len(p)-1]
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}
func cross(a, b, c point) float64 { return (b.lon-a.lon)*(c.lat-a.lat) - (b.lat-a.lat)*(c.lon-a.lon) }
func onSegment(a, b, p point) bool {
	return math.Abs(cross(a, b, p)) <= epsilon && p.lon >= math.Min(a.lon, b.lon)-epsilon && p.lon <= math.Max(a.lon, b.lon)+epsilon && p.lat >= math.Min(a.lat, b.lat)-epsilon && p.lat <= math.Max(a.lat, b.lat)+epsilon
}
func intersects(a, b, c, d point) bool {
	abC, abD, cdA, cdB := cross(a, b, c), cross(a, b, d), cross(c, d, a), cross(c, d, b)
	return ((abC > epsilon && abD < -epsilon || abC < -epsilon && abD > epsilon) && (cdA > epsilon && cdB < -epsilon || cdA < -epsilon && cdB > epsilon)) || onSegment(a, b, c) || onSegment(a, b, d) || onSegment(c, d, a) || onSegment(c, d, b)
}
func (p contour) validate() error {
	n := len(p)
	var area float64
	// ponytail: quadratic validation is sufficient for hand-drawn contours.
	for i, a := range p {
		b := p[(i+1)%n]
		if math.Abs(a.lon-b.lon) > 180 {
			return fmt.Errorf("contours crossing the antimeridian are not supported")
		}
		area += cross(p[0], a, b)
		for j := i + 1; j < n; j++ {
			c, d := p[j], p[(j+1)%n]
			if math.Abs(a.lon-c.lon) <= epsilon && math.Abs(a.lat-c.lat) <= epsilon {
				return fmt.Errorf("duplicate contour vertices")
			}
			if j == i+1 {
				if onSegment(a, b, d) || onSegment(c, d, a) {
					return fmt.Errorf("overlapping contour edges")
				}
			} else if i == 0 && j == n-1 {
				if onSegment(a, b, c) || onSegment(c, d, b) {
					return fmt.Errorf("overlapping contour edges")
				}
			} else if intersects(a, b, c, d) {
				return fmt.Errorf("self-intersecting contour")
			}
		}
	}
	if math.Abs(area) <= epsilon {
		return fmt.Errorf("contour has zero area")
	}
	return nil
}

// Boundary points count as inside.
func (p contour) contains(q point) bool {
	inside := false
	for i, a := range p {
		b := p[(i+1)%len(p)]
		if onSegment(a, b, q) {
			return true
		}
		if (a.lat > q.lat) != (b.lat > q.lat) && q.lon < (b.lon-a.lon)*(q.lat-a.lat)/(b.lat-a.lat)+a.lon {
			inside = !inside
		}
	}
	return inside
}
func tileCorner(z, x, y int) point {
	n := math.Exp2(float64(z))
	return point{float64(x)/n*360 - 180, math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/n))) * 180 / math.Pi}
}
func (p contour) includes(t tile) bool {
	return p.contains(tileCorner(t.z, t.x, t.y)) || p.contains(tileCorner(t.z, t.x+1, t.y)) || p.contains(tileCorner(t.z, t.x, t.y+1)) || p.contains(tileCorner(t.z, t.x+1, t.y+1))
}
func (p contour) walk(ctx context.Context, plan zoomPlan, visit func(tile) error) error {
	for y := plan.ymin; y <= plan.ymax; y++ {
		for x := plan.xmin; x <= plan.xmax; x++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			t := tile{plan.z, x, y}
			if p.includes(t) {
				if err := visit(t); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (p contour) plan(ctx context.Context, minZ, maxZ int) ([]zoomPlan, error) {
	if minZ < 0 || maxZ < minZ || maxZ > 30 {
		return nil, fmt.Errorf("zoom range must satisfy 0 <= minZ <= maxZ <= 30")
	}
	plans := make([]zoomPlan, 0, maxZ-minZ+1)
	for z := minZ; z <= maxZ; z++ {
		n := math.Exp2(float64(z))
		xmin, ymin, xmax, ymax := n, n, 0.0, 0.0
		for _, q := range p {
			x := (q.lon + 180) / 360 * n
			y := (1 - math.Asinh(math.Tan(q.lat*math.Pi/180))/math.Pi) / 2 * n
			xmin = math.Min(xmin, x)
			xmax = math.Max(xmax, x)
			ymin = math.Min(ymin, y)
			ymax = math.Max(ymax, y)
		}
		// Include both neighbours of grid boundaries, without retaining all selected tiles in memory.
		plan := zoomPlan{z: z, xmin: max(0, int(math.Floor(xmin))-1), xmax: min(int(n)-1, int(math.Floor(xmax))+1), ymin: max(0, int(math.Floor(ymin))-1), ymax: min(int(n)-1, int(math.Floor(ymax))+1)}
		if err := p.walk(ctx, plan, func(tile) error { plan.count++; return nil }); err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}
