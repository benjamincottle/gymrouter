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
- Infrastructure only. Gyms are built into the image, homes and their lines live on the device, and routing/risk
  defaults are compiled in (the old `[routing]`, `[risk]` and `[[gym]]` sections are rejected as unknown keys). Validate with:
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

## 4. Street map and basemap (automatic)

Nothing to do. After the timetables, the server downloads an OpenStreetMap extract of New South Wales (~270 MB, from
Geofabrik, rebuilt daily), builds the walking network of Greater Sydney from it (about 20 seconds, ~57 MB cached in
the data volume) and deletes the download; and it cuts the basemap (~63 MB) from the Protomaps daily build with the
`pmtiles` tool shipped in the image. Both cover the same area (west past Penrith, north to the Hawkesbury, south to
Waterfall) and refresh themselves every month or two. The container needs outbound HTTPS to `download.geofabrik.de`
and `build.protomaps.com`, and the data volume about 270 MB free while the street map is rebuilt. Until they arrive
(a minute or two) walks are straight-line estimates and the map is blank; if a download fails the app keeps working
and retries every few hours. `GET /api/status` shows `data` with their state and any error. The street map can be
fetched early from the app: open the server status in the footer and choose "Update now" on the Street map row (after
adding a path to OpenStreetMap; Geofabrik's extract has it the next day). To use another source or turn this off, see
`[data]` in `config.example.toml`: any extract that covers the area will do, and whatever it holds beyond the area is
left out.

## 5. Set up a device

```
docker compose exec gymrouter /app/gymrouter setup-link
```

Open the printed link on the phone (it carries the token in the URL fragment, which never reaches a
server log), then add your home and tick your gyms in Settings. The app finds the lines near home for each gym from the
timetable; you can review them. "Set up another device" in Settings shares a link or QR code.

## Operations

- Memory: idle ~170 MB live (the street network is ~70 MB of it). Asking for line suggestions (and the stop catalogue built after each timetable
  download) reads the whole network for a few seconds, one at a time, peaking near 310 MB; rebuilding the street
  network peaks near 340 MB. Measured under `GOMEMLIMIT=500MiB`, resident memory settles around 450 MB (Go keeps the
  headroom it's given), inside the 640 MB limit.
- Health: `GET /healthz` (public, minimal). Detail (feed ages, errors, upstream requests today, realtime
  match rates, configured lines with no trips): `GET /api/status` with the token.
- Upstream budget: realtime feeds are polled only while the app was used in the last 10 minutes, and
  capped at `realtime.daily_budget` requests per day (default 40,000 of the free plan's 60,000).
- Timetables refresh daily after `routing.static_refresh_hour` (default 05:00 Sydney time), keeping the
  last good copy if a download fails.
- Logs are JSON on stdout. They never include request bodies or query strings (which can contain home
  coordinates or addresses).
