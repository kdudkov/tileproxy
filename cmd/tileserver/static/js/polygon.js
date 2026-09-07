// Geometry is checked in geographic coordinates and in the map's Mercator projection.
const PolygonGeometry = (() => {
    const epsilon = 1e-12;
    const cross = (a, b, c) => (b.lng - a.lng) * (c.lat - a.lat) -
        (b.lat - a.lat) * (c.lng - a.lng);
    const onSegment = (a, b, p) => Math.abs(cross(a, b, p)) <= epsilon &&
        p.lng >= Math.min(a.lng, b.lng) - epsilon && p.lng <= Math.max(a.lng, b.lng) + epsilon &&
        p.lat >= Math.min(a.lat, b.lat) - epsilon && p.lat <= Math.max(a.lat, b.lat) + epsilon;
    const intersects = (a, b, c, d) => {
        const abC = cross(a, b, c), abD = cross(a, b, d);
        const cdA = cross(c, d, a), cdB = cross(c, d, b);
        return ((abC > epsilon && abD < -epsilon || abC < -epsilon && abD > epsilon) &&
            (cdA > epsilon && cdB < -epsilon || cdA < -epsilon && cdB > epsilon)) ||
            onSegment(a, b, c) || onSegment(a, b, d) || onSegment(c, d, a) || onSegment(c, d, b);
    };
    const area = points => points.reduce((sum, p, i) =>
        sum + cross(points[0], p, points[(i + 1) % points.length]), 0);

    function checkEdges(points, closed) {
        const n = points.length;
        const edges = closed ? n : n - 1;
        // ponytail: quadratic checks suit hand-drawn contours; use a sweep line for large imports.
        for (let i = 0; i < n; i++) {
            for (let j = i + 1; j < n; j++) {
                if (Math.abs(points[i].lng - points[j].lng) <= epsilon &&
                    Math.abs(points[i].lat - points[j].lat) <= epsilon) {
                    return 'Вершины контура не должны совпадать.';
                }
            }
        }
        for (let i = 0; i < edges; i++) {
            const a = points[i], b = points[(i + 1) % n];
            for (let j = i + 1; j < edges; j++) {
                const c = points[j], d = points[(j + 1) % n];
                if (j === i + 1) {
                    if (onSegment(a, b, d) || onSegment(c, d, a)) return 'Рёбра контура не должны накладываться.';
                } else if (closed && i === 0 && j === edges - 1) {
                    if (onSegment(a, b, c) || onSegment(c, d, b)) return 'Рёбра контура не должны накладываться.';
                } else if (intersects(a, b, c, d)) {
                    return 'Контур не должен пересекать или касаться самого себя.';
                }
            }
        }
        if (closed && Math.abs(area(points)) <= epsilon) return 'Площадь контура должна быть больше нуля.';
        return '';
    }

    function validate(points, closed = true) {
        if (closed && points.length < 3) return 'Необходимо минимум 3 точки для создания многоугольника.';
        if (points.some(p => !Number.isFinite(p.lat) || !Number.isFinite(p.lng) ||
            Math.abs(p.lat) > 90 || Math.abs(p.lng) > 180)) {
            return 'Координаты должны быть конечными: широта от −90 до 90, долгота от −180 до 180.';
        }
        const error = checkEdges(points, closed);
        if (error) return error;
        const projected = points.map(p => {
            const lat = Math.max(-85.0511287798, Math.min(85.0511287798, p.lat));
            return {lng: p.lng, lat: Math.log(Math.tan(Math.PI / 4 + lat * Math.PI / 360)) * 180 / Math.PI};
        });
        return checkEdges(projected, closed);
    }

    function toGeoJSON(points) {
        const error = validate(points);
        if (error) throw new Error(error);
        const coordinates = points.map(p => [p.lng, p.lat]);
        if (area(points) < 0) coordinates.reverse();
        coordinates.push([...coordinates[0]]);
        return {type: 'Polygon', coordinates: [coordinates]};
    }

    return {validate, toGeoJSON};
})();
