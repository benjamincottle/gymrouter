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
- No gyms are needed here: gyms, homes and their lines are set up in the app and live on the device.
  Optional `[[gym]]` entries are presets a device can import, and the server preloads their lines. Validate with:
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
server log), then add your home and each gym in Settings. The app suggests the lines for each gym from the
timetable; you review them. "Set up another device" in Settings shares a link or QR code.

## Operations

- Memory: idle ~90 MB. Asking for line suggestions (and the stop catalogue built after each timetable
  download) reads the whole network for a few seconds, one at a time, peaking near 310 MB.
- Health: `GET /healthz` (public, minimal). Detail (feed ages, errors, upstream requests today, realtime
  match rates, configured lines with no trips): `GET /api/status` with the token.
- Upstream budget: realtime feeds are polled only while the app was used in the last 10 minutes, and
  capped at `realtime.daily_budget` requests per day (default 40,000 of the free plan's 60,000).
- Timetables refresh daily after `routing.static_refresh_hour` (default 05:00 Sydney time), keeping the
  last good copy if a download fails.
- Logs are JSON on stdout. They never include request bodies or query strings (which can contain home
  coordinates or addresses).
