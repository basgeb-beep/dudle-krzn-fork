#!/usr/bin/env bash
# Installs/updates Dudle-NG in /opt/dudle on the server. Run as root (or with sudo).
# Needs: git, docker with the compose plugin, and an existing nginx.
set -euo pipefail

REPO="${REPO:-https://github.com/basgeb-beep/dudle-krzn-fork.git}"
DIR="${DIR:-/opt/dudle}"

for c in git docker; do
  command -v "$c" >/dev/null || { echo "missing: $c" >&2; exit 1; }
done
docker compose version >/dev/null || { echo "missing: docker compose plugin" >&2; exit 1; }

mkdir -p "$DIR"
if [ -d "$DIR/src/.git" ]; then
  git -C "$DIR/src" pull --ff-only
else
  git clone "$REPO" "$DIR/src"
fi

docker compose -f "$DIR/src/deploy/docker-compose.yml" up -d --build
cp "$DIR/src/deploy/nginx-dudle.conf" "$DIR/nginx-dudle.conf"

echo
echo "Dudle-NG is running on 127.0.0.1:8080."
if grep -rqs "nginx-dudle.conf" /etc/nginx 2>/dev/null; then
  echo "nginx already includes $DIR/nginx-dudle.conf"
else
  echo "Add this line inside the server { } block of your site, then run: nginx -t && systemctl reload nginx"
  echo "    include $DIR/nginx-dudle.conf;"
fi
