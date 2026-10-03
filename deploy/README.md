# Deploying Gym Router

The image is built and published by `.github/workflows/build.yml` to
`ghcr.io/benjamincottle/gymrouter` (`linux/arm64`, distroless, runs as uid 65532). Deployment is a
compose stack behind Traefik; the files below live in the private deploy repo.

## 1. Secrets (Ansible Vault)

| Variable | What |
|---|---|
| `TFNSW_API_KEY` | TfNSW Open Data Hub API key |
| `GYMROUTER_TOKEN` | Access token; generate once with `docker run --rm ghcr.io/benjamincottle/gymrouter new-token` |

Template them into a `.env` next to the compose file (mode 0600). Changing `GYMROUTER_TOKEN` signs out
every device.

## 2. Config

Start from `config.example.toml` in the app repo and keep the real file in the deploy repo:

- `server.public_url = "https://gymrouter.example.com"` (used by `setup-link`)
- `server.data_dir = "/data"`, `server.listen = ":8080"`, `server.trust_proxy = true` (Traefik sets
  `X-Forwarded-For`)
- Each gym's `lines` should include the lines near home, e.g. `"bus 999"`, `"train T4"`, as well as
  the gym-side ones. Validate with:
  ```
  docker run --rm -v ./config.toml:/c.toml:ro ghcr.io/benjamincottle/gymrouter check-config --config /c.toml
  ```

## 3. Compose

Copy `compose.example.yaml` and adjust the host rule if needed. On first start the container downloads
the timetables (~300 MB) before it reports healthy; Traefik routes to it once it does.

```
docker compose up -d
docker compose logs -f gymrouter     # "timetable loaded", then "listening"
```

## 4. Map (optional, recommended)

The basemap is a PMTiles extract of Greater Sydney (~63 MB) in the data volume as `map.pmtiles`.
Without it, the map shows routes and vehicles on a blank background. Create or refresh it (every month
or two) with the PMTiles CLI image, writing as the app's user:

```
docker run --rm --user 65532:65532 -v <project>_gymrouter_data:/data ghcr.io/protomaps/go-pmtiles:latest \
  extract https://build.protomaps.com/$(date -d yesterday +%Y%m%d).pmtiles /data/map.pmtiles \
  --bbox=150.55,-34.15,151.35,-33.45
```

(`<project>` is the compose project name, usually the directory name.) The app picks up the new file
on the next request; the browser revalidates it daily.

## 5. Set up a device

```
docker compose exec gymrouter /app/gymrouter setup-link
```

Open the printed link on the phone (it carries the token in the URL fragment, which never reaches a
server log), then add home in Settings. "Set up another device" in Settings shares a link or QR code.

## Operations

- Health: `GET /healthz` (public, minimal). Detail (feed ages, errors, upstream requests today, realtime
  match rates, configured lines with no trips): `GET /api/status` with the token.
- Upstream budget: realtime feeds are polled only while the app was used in the last 10 minutes, and
  capped at `realtime.daily_budget` requests per day (default 40,000 of the free plan's 60,000).
- Timetables refresh daily after `routing.static_refresh_hour` (default 05:00 Sydney time), keeping the
  last good copy if a download fails.
- Logs are JSON on stdout. They never include request bodies or query strings (which can contain home
  coordinates or addresses).
