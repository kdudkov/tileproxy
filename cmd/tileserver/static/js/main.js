var map = null;
var grid = null;

const app = Vue.createApp({
    data: function () {
        return {
            layers: null,
            ts: 0,
            zoom: 10,
            keys: new Set(),
            dz: 2,
            filename: "tiles",
            drawingMode: false,
            polygonError: "",
            polygon: null,
            polygonPoints: [],
            markers: [],
            coords: null,
            locked_unit_uid: null,
            layerName: null,
        }
    },

    mounted() {
        map = L.map('map');

        L.control.scale({metric: true}).addTo(map);

        map.setView([60, 30.8], this.zoom);

        grid = new L.GridLayer({tileSize: 256 / (1 << this.dz), zIndex: 0});
        grid.createTile = this.draw_tile;

        this.get_layers();
        map.on('click', this.onClick);
        map.on('zoomend', this.onZoom);
        map.on('mousemove', this.onMouseMove);
    },

    methods: {
        get_layers: function () {
            let th = this;
            fetch('/layers')
                .then(resp => resp.json())
                .then(data => {
                    th.layers = Vue.markRaw(L.control.layers({}, {}, {hideSingleBase: true}));
                    th.layers.addTo(map);

                    let first = true;
                    data.forEach(function (i) {
                        let opts = {
                            maxZoom: i.max_zoom || 21,
                            minZoom: i.min_zoom || 1,
                        };

                        if (i.parts) {
                            opts["subdomains"] = i.parts;
                        }

                        console.log(opts);

                        let l = L.tileLayer(i.url, opts);

                        if (i.file) {
                            th.layers.addOverlay(l, i.name);
                        } else {
                            th.layers.addBaseLayer(l, i.name);
                            if (first) {
                                first = false;
                                l.addTo(map);
                            }
                        }
                    });

                    th.layers.addOverlay(grid, "grid");
                    grid.bringToFront();
                });
        },

        draw_tile: function (coords) {
            let key = [coords.z + this.dz, coords.x, coords.y].join('/');

            const tile = document.createElement('div');

            tile.style.outline = '1px solid green';
            if (this.keys.has(key)) {
                tile.style.backgroundColor = 'rgba(255,0,0,0.1)';
            }
            tile.style.fontSize = '6pt';
            // tile.innerHTML = key;
            return tile;
        },

        onClick: function (e) {
            if (this.drawingMode) {
                this.addPolygonPoint(e.latlng);
                return;
            }

            let ts = 256 / (1 << this.dz);
            let p = map.project(e.latlng, map.getZoom());
            let key = [map.getZoom() + this.dz, Math.floor(p.x / ts), Math.floor(p.y / ts)].join('/');
            // console.log(key);

            if (this.keys.has(key)) {
                this.keys.delete(key);
            } else {
                this.keys.add(key);
            }
            this.ts = this.keys.size;

            grid.redraw();
        },

        onMouseMove: function (e) {
            this.coords = e.latlng;
        },

        onZoom: function (e) {
            this.zoom = map.getZoom();
        },

        copy_up: function () {
            let z = map.getZoom() + this.dz - 1;
            for (let k of this.keys) {
                if (k.startsWith(z + "/")) {
                    let n = k.split('/');
                    this.keys.add([z + 1, n[1] * 2, n[2] * 2].join('/'));
                    this.keys.add([z + 1, n[1] * 2 + 1, n[2] * 2].join('/'));
                    this.keys.add([z + 1, n[1] * 2, n[2] * 2 + 1].join('/'));
                    this.keys.add([z + 1, n[1] * 2 + 1, n[2] * 2 + 1].join('/'));
                }
            }
            this.ts = this.keys.size;
            grid.redraw();
        },

        print: function () {
            console.log()
            window.open('data:text/csv;charset=utf-8,' + encodeURI(Array.from(this.keys).join("\n")));
        },
        redraw_all: function () {
            map.eachLayer(function (layer) {
                layer.redraw();
            });
        },
        clear: function () {
            this.keys.clear();
            grid.redraw();
        },
        clear_zoom: function () {
            let z = map.getZoom()
            for (let k of this.keys) {
                if (k.startsWith(z + "/")) {
                    this.keys.delete(key);
                }
            }
            this.ts = this.keys.size;
            grid.redraw();
        },

        toggleDrawing: function () {
            if (this.drawingMode) {
                this.closePolygon();
                return;
            }
            this.drawingMode = true;
            this.polygonError = '';
            this.renderPolygon();
        },

        addPolygonPoint: function (latlng) {
            const points = [...this.polygonPoints, latlng];
            this.polygonError = PolygonGeometry.validate(points, false);
            if (this.polygonError) return;
            this.polygonPoints = points;
            this.renderPolygon();
        },

        closePolygon: function () {
            this.polygonError = PolygonGeometry.validate(this.polygonPoints);
            if (this.polygonError) return;
            this.drawingMode = false;
            this.renderPolygon();
        },

        // Leaflet must receive the same instances when registering and removing map listeners.
        renderPolygon: function () {
            this.markers.forEach(m => map.removeLayer(m));
            this.markers = [];
            if (this.polygon) map.removeLayer(this.polygon);
            this.polygon = null;

            if (this.polygonPoints.length > 1) {
                const options = {color: '#3388ff', weight: 2, fillOpacity: 0.2};
                if (this.drawingMode) {
                    this.polygon = Vue.markRaw(L.polyline(this.polygonPoints, {...options, interactive: false}).addTo(map));
                } else {
                    this.polygon = Vue.markRaw(L.polygon(this.polygonPoints, {...options, bubblingMouseEvents: false}).addTo(map));
                    this.polygon.on('click', e => this.insertVertex(e.latlng));
                }
            }
            if (this.drawingMode) {
                this.polygonPoints.forEach(point => {
                    this.markers.push(Vue.markRaw(L.circleMarker(point, {
                        radius: 8, fillColor: '#3388ff', color: '#fff', weight: 2,
                        fillOpacity: 0.8, interactive: false
                    }).addTo(map)));
                });
            } else {
                this.makeDraggable();
            }
        },

        makeDraggable: function () {
            this.markers.forEach(m => map.removeLayer(m));
            this.markers = [];
            const icon = L.divIcon({
                className: 'polygon-vertex-marker',
                html: '<div style="width: 16px; height: 16px; background: #3388ff; border: 2px solid #fff; border-radius: 50%; cursor: move;"></div>',
                iconSize: [16, 16],
                iconAnchor: [8, 8]
            });
            this.polygonPoints.forEach((point, index) => {
                const marker = Vue.markRaw(L.marker(point, {icon: icon, draggable: true}).addTo(map));
                marker.on('drag', e => {
                    const points = this.polygonPoints.slice();
                    points[index] = e.target.getLatLng();
                    this.polygonError = PolygonGeometry.validate(points);
                    if (this.polygonError) {
                        e.target.setLatLng(this.polygonPoints[index]);
                        return;
                    }
                    this.polygonPoints = points;
                    this.polygon.setLatLngs(points);
                });
                this.markers.push(marker);
            });
        },

        insertVertex: function (latlng) {
            let minDist = Infinity;
            let insertIndex = 0;
            for (let i = 0; i < this.polygonPoints.length; i++) {
                const p1 = map.latLngToLayerPoint(this.polygonPoints[i]);
                const p2 = map.latLngToLayerPoint(this.polygonPoints[(i + 1) % this.polygonPoints.length]);
                const p = map.latLngToLayerPoint(latlng);
                const dist = this.distanceToSegment(
                    {lng: p.x, lat: p.y}, {lng: p1.x, lat: p1.y}, {lng: p2.x, lat: p2.y});
                if (dist < minDist) {
                    minDist = dist;
                    insertIndex = i + 1;
                }
            }
            const points = this.polygonPoints.slice();
            points.splice(insertIndex, 0, latlng);
            this.polygonError = PolygonGeometry.validate(points);
            if (this.polygonError) return;
            this.polygonPoints = points;
            this.polygon.setLatLngs(points);
            this.makeDraggable();
        },

        downloadPolygon: function () {
            if (this.drawingMode) return;
            this.polygonError = PolygonGeometry.validate(this.polygonPoints);
            if (this.polygonError) return;
            const data = PolygonGeometry.toGeoJSON(this.polygonPoints);
            const blob = new Blob([JSON.stringify(data, null, 2) + '\n'], {type: 'application/geo+json'});
            const url = URL.createObjectURL(blob);
            const link = document.createElement('a');
            link.href = url;
            link.download = 'contour.geojson';
            document.body.appendChild(link);
            link.click();
            link.remove();
            // Keep the URL alive until the browser has started the download.
            setTimeout(() => URL.revokeObjectURL(url), 1000);
        },

        distanceToSegment: function (point, p1, p2) {
            const x = point.lng, y = point.lat;
            const x1 = p1.lng, y1 = p1.lat;
            const x2 = p2.lng, y2 = p2.lat;
            const A = x - x1, B = y - y1, C = x2 - x1, D = y2 - y1;
            const dot = A * C + B * D;
            const lenSq = C * C + D * D;
            const param = lenSq !== 0 ? dot / lenSq : -1;
            let xx, yy;
            if (param < 0) { xx = x1; yy = y1; }
            else if (param > 1) { xx = x2; yy = y2; }
            else { xx = x1 + param * C; yy = y1 + param * D; }
            const dx = x - xx, dy = y - yy;
            return dx * dx + dy * dy;
        },

        clearPolygon: function () {
            this.polygonPoints = [];
            this.drawingMode = false;
            this.polygonError = '';
            this.renderPolygon();
        },

        removeLastPoint: function () {
            if (!this.drawingMode || this.polygonPoints.length === 0) return;
            this.polygonPoints.pop();
            this.polygonError = '';
            this.renderPolygon();
        },

        printCoordsll: function (latlng) {
            if (!latlng) return '--';
            return latlng.lat.toFixed(5) + ', ' + latlng.lng.toFixed(5);
        }
    }
});

app.mount('#app');