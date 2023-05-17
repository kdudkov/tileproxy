let app = new Vue({
    el: '#app',
    data: {
        map: null,
        layers: null,
        ts: 0,
        zoom: 10,
        keys: new Set(),
        coeff: 2,
        filename: "tiles",
    },

    mounted() {
        this.map = L.map('map');

        L.control.scale({metric: true}).addTo(this.map);

        this.map.setView([60, 30.8], this.zoom);

        L.GridLayer.GridDebug = L.GridLayer.extend({
            createTile: this.draw_tile,
        });


        this.get_layers();
        this.map.on('click', this.click);
        this.map.on('zoom', this.onZoom)
    },

    methods: {
        get_layers: function () {
            let th = this;
            fetch('/layers')
                .then(function (response) {
                    return response.json()
                })
                .then(function (data) {
                    th.layers = L.control.layers({}, null, {hideSingleBase: true});
                    th.layers.addTo(th.map);

                    let first = true;
                    data.forEach(function (i) {
                        console.log(i);
                        let opts = {
                            maxZoom: i.maxzoom,
                            minZoom: i.minzoom
                        };

                        if (i.parts) {
                            opts["subdomains"] = i.parts;
                        }

                        let l = L.tileLayer(i.url, opts);

                        if (i.file) {
                            th.layers.addOverlay(l, i.name);
                        } else {
                            th.layers.addBaseLayer(l, i.name);
                            if (first) {
                                first = false;
                                l.addTo(th.map);
                            }
                        }
                    });

                    let grid = new L.GridLayer.GridDebug({tileSize: 256 / this.coeff, zIndex: 0});
                    layers.addOverlay(grid, "grid");
                    grid.bringToFront();
                });
        },

        draw_tile: function (coords) {
            let key = [coords.z + this.coeff - 1, coords.x, coords.y].join('/');

            const tile = document.createElement('div');

            tile.style.outline = '1px solid green';
            if (this.keys.has(key)) {
                tile.style.backgroundColor = 'rgba(255,0,0,0.1)';
            }
            tile.style.fontSize = '10pt';
            tile.innerHTML = key;
            return tile;
        },

        click: function (e) {
            // console.log(e);
            let ts = this.grid.getTileSize().x;
            let p = this.map.project(e.latlng, this.map.getZoom());
            let key = [this.map.getZoom() + this.coeff - 1, Math.floor(p.x / ts), Math.floor(p.y / ts)].join('/');
            console.log(key);

            if (this.keys.has(key)) {
                this.keys.delete(key);
            } else {
                this.keys.add(key);
            }
            this.ts = this.keys.size;
            this.grid.redraw();
        },

        onZoom: function (e) {
            this.zoom = this.map.getZoom();
        },

        copy_up: function () {
            let z = this.map.getZoom() + this.coeff - 2;
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
            this.grid.redraw();
        },

        print: function () {
            console.log()
            window.open('data:text/csv;charset=utf-8,' + encodeURI(Array.from(this.keys).join("\n")));
        },
        redraw_all: function () {
            this.map.eachLayer(function (layer) {
                layer.redraw();
            });
        },
        clear: function () {
            this.keys.clear();
            this.grid.redraw();
        }
    }
});