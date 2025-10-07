package model

type Tile struct {
	X int
	Y int
	Z int
}

func (t *Tile) InRect(t1, t2 *Tile) bool {
	x1 := t.X * (1 << (19 - t.Z))
	y1 := t.Y * (1 << (19 - t.Z))

	xmin := t1.X * (1 << (19 - t1.Z))
	xmax := (t2.X + 1) * (1 << (19 - t2.Z))

	ymin := t1.Y * (1 << (19 - t1.Z))
	ymax := (t2.Y + 1) * (1 << (19 - t2.Z))

	return x1 >= xmin && x1 < xmax && y1 >= ymin && y1 < ymax
}
