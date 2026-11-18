# Deployment (nginx + Docker, under `/dudle/`)

Result: `https://<your-host>/dudle/`, open to everyone (no login), data in a Docker volume.

Requirements on the server: `git`, Docker with the compose plugin, nginx with TLS already set up for the host.

```sh
curl -fsSL https://raw.githubusercontent.com/basgeb-beep/dudle-krzn-fork/master/deploy/install.sh | sudo bash
```

The script clones the repo to `/opt/dudle/src`, builds and starts the container on `127.0.0.1:8080` and writes `/opt/dudle/nginx-dudle.conf`.
Then add this to the `server { }` block of the site and reload:

```nginx
include /opt/dudle/nginx-dudle.conf;
```
```sh
sudo nginx -t && sudo systemctl reload nginx
```

Check: `curl -i https://<your-host>/dudle/healthz` returns `ok`.

## Update
Run the same `install.sh` again (pulls, rebuilds, restarts; data is kept).

## Backup
`docker run --rm -v dudle_dudle-data:/data -v "$PWD":/out alpine tar czf /out/dudle-backup.tgz -C /data .`

## Remove
`docker compose -f /opt/dudle/src/deploy/docker-compose.yml down` (add `-v` to also delete the data).
