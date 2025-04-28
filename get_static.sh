#!/bin/bash

d="./cmd/tileserver/static"

# bootstrap
curl https://cdn.jsdelivr.net/npm/bootstrap@5.3/dist/css/bootstrap.min.css -o $d/css/bootstrap.min.css
curl https://cdn.jsdelivr.net/npm/bootstrap@5.3/dist/js/bootstrap.bundle.min.js -o $d/js/bootstrap.bundle.min.js

# icons
#curl -L https://github.com/twbs/icons/releases/download/v1.11.3/bootstrap-icons-1.11.3.zip -o icons.zip
#unzip -o -d /tmp/icons/ icons.zip
#cp /tmp/icons/bootstrap-icons-1.11.3/font/fonts/* $d/css/fonts/
#cp /tmp/icons/bootstrap-icons-1.11.3/font/bootstrap-icons.min.css $d/css/
#rm icons.zip

# vue
curl -L https://unpkg.com/vue@3/dist/vue.global.prod.js -o $d/js/vue.js

# leaflet
curl https://cdn.jsdelivr.net/npm/leaflet@1.9/dist/leaflet.min.css -o $d/css/leaflet.css
curl https://cdn.jsdelivr.net/npm/leaflet@1.9/dist/leaflet.min.js -o $d/js/leaflet.js

for name in layers layers-2x marker-icon marker-icon-2x marker-shadow; do
  curl https://unpkg.com/leaflet@1.9/dist/images/${name}.png -o $d/css/images/${name}.png
done;


