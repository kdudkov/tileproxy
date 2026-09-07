const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {test} = require('node:test');

function setup() {
    let options;
    const downloads = [];
    const urls = [];
    const timers = [];
    const layer = (point, options) => ({
        point, options, events: {},
        addTo() {return this;},
        on(name, fn) {this.events[name] = fn; return this;},
        setLatLngs(points) {this.points = points;},
        setLatLng(point) {this.point = point;},
        getLatLng() {return this.point;}
    });
    const context = vm.createContext({
        L: {polygon: layer, polyline: layer, circleMarker: layer, marker: layer, divIcon() {}},
        Blob,
        URL: {
            createObjectURL(blob) {urls.push(blob); return 'blob:contour';},
            revokeObjectURL(url) {assert.equal(url, 'blob:contour');}
        },
        document: {
            body: {appendChild() {}},
            createElement() {return {click() {downloads.push(this);}, remove() {}};}
        },
        setTimeout(fn) {timers.push(fn);}
    });
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../cmd/tileserver/static/js/vue.js'), 'utf8'), context);
    context.Vue.createApp = value => {options = value; return {mount() {}};};
    for (const file of ['polygon.js', 'main.js']) {
        vm.runInContext(fs.readFileSync(path.join(__dirname, '../cmd/tileserver/static/js', file), 'utf8'), context);
    }
    context.map = {removeLayer() {}, latLngToLayerPoint(p) {return {x: p.lng, y: p.lat};}};
    const geometry = vm.runInContext('PolygonGeometry', context);
    return {app: context.Vue.reactive(Object.assign(options.data(), options.methods)), geometry, downloads, urls, timers, Vue: context.Vue};
}
const points = coordinates => coordinates.map(([lng, lat]) => ({lng, lat}));
const square = () => points([[0, 0], [4, 0], [4, 4], [0, 4]]);

test('clicking the first vertex closes a valid draft without adding a duplicate point', () => {
    const {app} = setup();
    app.toggleDrawing();
    for (const p of square()) app.addPolygonPoint(p);
    const first = app.markers[0];
    assert.equal(first.options.interactive, true);
    assert.equal(first.options.bubblingMouseEvents, false);
    assert.ok(app.markers.slice(1).every(m => !m.options.interactive));
    first.events.click();
    assert.equal(app.drawingMode, false);
    assert.equal(app.polygonError, '');
    assert.deepEqual(JSON.parse(JSON.stringify(app.polygonPoints)), square());
});

test('clicking the first vertex preserves drafts with too few points or an invalid closing edge', () => {
    for (const coordinates of [
        [[0, 0]],
        [[0, 0], [4, 0]],
        [[0, 0], [4, 0], [0, 4], [4, 4]]
    ]) {
        const {app} = setup();
        app.toggleDrawing();
        const draft = points(coordinates);
        for (const p of draft) app.addPolygonPoint(p);
        app.markers[0].events.click();
        assert.equal(app.drawingMode, true);
        assert.ok(app.polygonError);
        assert.deepEqual(JSON.parse(JSON.stringify(app.polygonPoints)), draft);
    }
});

test('rejects crossings, touches, overlaps, duplicates and degenerate rings', () => {
    const {geometry} = setup();
    for (const coordinates of [
        [[0, 0], [4, 4], [0, 4], [4, 0]],
        [[0, 0], [4, 0], [4, 4], [2, 0], [0, 4]],
        [[0, 0], [4, 0], [2, 0], [2, 4]],
        [[0, 0], [4, 0], [4, 4], [0, 0]],
        [[0, 0], [1, 1], [2, 2]],
        [[0, 0], [1, 1]],
        [[0, 0], [NaN, 1], [1, 0]],
        [[0, 0], [181, 1], [1, 0]]
    ]) assert.ok(geometry.validate(points(coordinates)), JSON.stringify(coordinates));
    assert.equal(geometry.validate(square()), '');
    assert.equal(geometry.validate(points([[0, 0], [2, 0], [4, 0], [4, 4], [0, 4]])), '');
    assert.equal(geometry.validate(points([[0, 0], [4, 0], [4, 4], [2, 2], [0, 4]])), '');
});

test('drawing rejects crossing segments and failed closure preserves draft', () => {
    const {app} = setup();
    app.toggleDrawing();
    for (const p of points([[0, 0], [4, 4], [0, 4]])) app.addPolygonPoint(p);
    app.addPolygonPoint({lng: 4, lat: 0});
    assert.equal(app.polygonPoints.length, 3);
    assert.ok(app.polygonError);
    app.clearPolygon();
    app.toggleDrawing();
    // The open path is simple, but its closing edge crosses the middle edge.
    for (const p of points([[0, 0], [4, 0], [0, 4], [4, 4]])) app.addPolygonPoint(p);
    app.toggleDrawing();
    assert.equal(app.drawingMode, true);
    assert.equal(app.polygonPoints.length, 4);
    assert.ok(app.polygonError);
    app.removeLastPoint();
    app.toggleDrawing();
    assert.equal(app.drawingMode, false);
    assert.equal(app.polygonError, '');
});

test('invalid drag and duplicate insertion preserve the valid contour', () => {
    const {app} = setup();
    app.polygonPoints = square();
    app.renderPolygon();
    const marker = app.markers[1];
    marker.point = {lng: -1, lat: 3};
    marker.events.drag({target: marker});
    assert.deepEqual(JSON.parse(JSON.stringify(app.polygonPoints)), square());
    assert.deepEqual(marker.point, square()[1]);
    assert.ok(app.polygonError);
    marker.point = {lng: 5, lat: 0};
    marker.events.drag({target: marker});
    assert.equal(app.polygonPoints[1].lng, 5);
    assert.equal(app.polygonError, '');
    app.insertVertex(app.polygonPoints[0]);
    assert.equal(app.polygonPoints.length, 4);
    assert.ok(app.polygonError);
    app.insertVertex({lng: 2, lat: 0});
    assert.equal(app.polygonPoints.length, 5);
    assert.equal(app.polygonError, '');
});

test('download contains a closed counterclockwise GeoJSON ring in longitude/latitude order', async () => {
    const {app, downloads, urls, timers} = setup();
    const original = points([[30, 60], [30, 61], [31, 61], [31, 60]]);
    app.polygonPoints = original;
    app.downloadPolygon();
    assert.equal(downloads.length, 1);
    assert.equal(downloads[0].download, 'contour.geojson');
    assert.equal(downloads[0].href, 'blob:contour');
    assert.equal(urls[0].type, 'application/geo+json');
    const data = JSON.parse(await urls[0].text());
    assert.deepEqual(data, {type: 'Polygon', coordinates: [[[31, 60], [31, 61], [30, 61], [30, 60], [31, 60]]]});
    assert.deepEqual(app.polygonPoints, original);
    timers.forEach(fn => fn());
    app.drawingMode = true;
    app.downloadPolygon();
    assert.equal(downloads.length, 1);
    app.drawingMode = false;
    app.polygonPoints = points([[0, 0], [1, 1], [2, 2]]);
    app.downloadPolygon();
    assert.equal(downloads.length, 1);
    assert.ok(app.polygonError);
});


test('Leaflet layers retain their identity through reactive state and marker replacement', () => {
    const {app, Vue} = setup();
    app.toggleDrawing();
    for (const p of square()) app.addPolygonPoint(p);
    assert.equal(Vue.isReactive(app.polygon), false);
    assert.ok(app.markers.every(marker => !Vue.isReactive(marker)));
    app.closePolygon();
    const oldMarkers = app.markers.slice();
    for (const marker of oldMarkers) {
        // A proxy here would not match the listener context registered by addTo(map).
        assert.equal(Vue.isReactive(marker), false);
    }
    app.insertVertex({lng: 2, lat: 0});
    assert.equal(app.markers.length, 5);
    assert.ok(app.markers.every(marker => !Vue.isReactive(marker) && !oldMarkers.includes(marker)));
    assert.equal(Vue.isReactive(app.polygon), false);
    assert.equal(Vue.isReactive(app.polygonPoints), true);
});
