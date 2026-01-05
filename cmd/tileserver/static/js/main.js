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
    },

    methods: {
        get_layers: function () {
            let th = this;
            fetch('/layers')
                .then(resp => resp.json())
                .then(data => {
                    th.layers = L.control.layers({}, {}, {hideSingleBase: true});
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
        }
    }
});

app.mount('#app');