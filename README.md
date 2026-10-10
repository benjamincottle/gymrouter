# Gym Router

Public transport routing for a handful of destinations (bouldering gyms), using Transport for NSW
open data. Instead of searching the whole network like a general planner, it checks every
option within each gym's own set of lines against live data, using your own walking and transfer
times. Sixteen Sydney climbing gyms are built in; you add your home and the app finds the lines near it from the
timetable. Homes and their lines stay on your device. See [docs/spec.md](docs/spec.md).

Status: all milestones built (router, server, web app, live map, in-trip mode, deployment). Field
validation against Opal/Google is next. Deploying: [deploy/README.md](deploy/README.md).

## Commands
```
gymrouter serve          run the server
gymrouter setup-link     print the link that sets up a device
gymrouter new-token      generate an access token
gymrouter check-config   validate a config file
gymrouter healthcheck    probe the local server (the container's HEALTHCHECK)
gymrouter suggest-lines  find candidate lines between two places (the app does this too)
gymrouter plan           plan a trip from the command line, optionally with live data
gymrouter make-fixture   build the trimmed real-data test fixture
```

## Running
1. Get a free API key from the [TfNSW Open Data Hub](https://opendata.transport.nsw.gov.au).
2. Copy `config.example.toml` and adjust it (listen address, data directory, public URL). It holds infrastructure only.
3. Set the secrets in the environment and start the server:
   ```
   export TFNSW_API_KEY=…  GYMROUTER_TOKEN=$(gymrouter new-token)
   gymrouter serve --config config.toml
   ```
   On first start it downloads the timetables (~300 MB) into `server.data_dir`.
4. That's all: the server downloads the rest itself in the background. After the timetables (the first start waits for
   these) it fetches an OpenStreetMap extract for walking (~60 MB, built into a street network) and cuts the Sydney basemap
   from the Protomaps build with the `pmtiles` tool (~63 MB; in the Docker image, or install it and put it on `PATH`). Both
   refresh themselves every month or two. Until they arrive, walks are straight-line estimates and the map is blank.
5. Run `gymrouter setup-link` and open the link on your phone. In Settings, add your home and tick
   your gyms; the app finds the lines near home for each. Other gyms can be added by address. To change the built-in gyms,
   edit `internal/gyms/gyms.toml`.

API: [docs/api.md](docs/api.md).

## Security and privacy
- No accounts. Access needs a random 256-bit token, delivered in the setup link's URL fragment, which browsers
  never send to servers. The server keeps only the token's SHA-256 digest.
- Home locations and personal walking times stay on the device and arrive only in request bodies,
  which are never logged or stored. This repository contains no personal data.
- Walks follow real streets (OpenStreetMap), routed on the server from a graph built from a public extract; your
  home location is only ever used in memory for that.
- Only the server talks to TfNSW, on its own schedule, and only while the app is in use. A daily request
  budget caps upstream calls, so public traffic can't exhaust the API key's quota.

## Development
```
(cd web && npm ci && npm run build)             # builds the frontend into internal/webui/dist (embedded)
go build -o bin/gymrouter ./cmd/gymrouter
go test ./...                                   # unit + golden tests (no network)
(cd web && npm test)                            # frontend unit tests
go test ./internal/plan -run Fixture -update    # regenerate golden files
```
Without the frontend build, the server still runs and serves only the API. For frontend work,
`npm run dev` in `web/` proxies the API to a server on 127.0.0.1:18080.
Requires Go 1.27+. Regenerating protobuf bindings needs `protoc` and `protoc-gen-go`
(`protoc --go_out=… proto/gtfs-realtime.proto`).

## Data and credits
- Contains Transport for NSW data, © Transport for NSW, licensed under
  [CC BY 4.0](https://opendata.transport.nsw.gov.au/datalicence).
- Map data © OpenStreetMap contributors (ODbL), via [Protomaps](https://protomaps.com) basemaps (BSD-3).
- Map fonts: Noto Sans (SIL OFL, `web/public/basemap/fonts/OFL.txt`); map icons derived from tangrams/icons (MIT,
  `web/public/basemap/sprites/LICENSE.md`). App font: Archivo (SIL OFL).
