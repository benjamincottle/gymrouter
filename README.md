# Gym Router

Public transport routing for a handful of destinations (bouldering gyms), using Transport for NSW
open data. Instead of searching the whole network like a general planner, it checks every
option within each gym's own set of lines against live data, using your own walking and transfer
times. See [docs/spec.md](docs/spec.md).

Status: router, server and web app work. The live map (milestone 5), in-trip mode (6) and deployment (7) are next.

## Commands
```
gymrouter serve          run the server
gymrouter setup-link     print the link that sets up a device
gymrouter new-token      generate an access token
gymrouter check-config   validate a config file
gymrouter suggest-lines  find candidate lines between two places (run locally)
gymrouter plan           plan a trip from the command line, optionally with live data
gymrouter make-fixture   build the trimmed real-data test fixture
```

## Running
1. Get a free API key from the [TfNSW Open Data Hub](https://opendata.transport.nsw.gov.au).
2. Copy `config.example.toml` and add the lines near your home to each gym (keep this file private).
3. Set the secrets in the environment and start the server:
   ```
   export TFNSW_API_KEY=…  GYMROUTER_TOKEN=$(gymrouter new-token)
   gymrouter serve --config config.toml
   ```
   On first start it downloads the timetables (~300 MB) into `server.data_dir`.
4. Run `gymrouter setup-link` and open the link on your phone.

API: [docs/api.md](docs/api.md).

## Security and privacy
- No accounts. Access needs a random 256-bit token, delivered in the setup link's URL fragment, which browsers
  never send to servers. The server keeps only the token's SHA-256 digest.
- Home locations and personal walking times stay on the device and arrive only in request bodies,
  which are never logged or stored. This repository contains no personal data.
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

## Data
Contains Transport for NSW data, © Transport for NSW, licensed under
[CC BY 4.0](https://opendata.transport.nsw.gov.au/datalicence).
