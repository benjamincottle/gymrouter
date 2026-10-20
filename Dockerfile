# syntax=docker/dockerfile:1

# The build images are pinned by digest (Dependabot keeps them current), so what's built only changes with a
# commit: a new Go or Node arrives as a pull request, and CI builds on it before anything is published.

# --- Frontend: built once on the build platform (the output is platform-independent) ---
FROM --platform=$BUILDPLATFORM node:26-trixie-slim@sha256:0d1e33586362bb9e608c4e751e7a9e7edb55c9ac11461ed5666af696cc461dd4 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
# --ignore-scripts: no dependency gets to run install scripts.
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY web/ ./
# Vite writes to ../internal/webui/dist, which the Go binary embeds.
RUN npm run build

# --- Server: cross-compiled from the build platform (fast on any runner) ---
FROM --platform=$BUILDPLATFORM golang:1.27-trixie@sha256:47667934874913a449b96faa7ef4777fa285d9ec29126ee80ed61c45b7cccf0e AS build
ARG TARGETOS TARGETARCH
# The commit being built (CI passes github.sha); the footer shows it. The context has no .git to read it from.
ARG REVISION=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X github.com/benjamincottle/gymrouter/internal/version.Revision=${REVISION}" \
       -o /out/gymrouter ./cmd/gymrouter \
 && mkdir -p /out/data

# --- The pmtiles tool: the server runs it to cut the Sydney basemap out of the Protomaps build ---
# A static Go binary, pinned by digest (Dependabot keeps it current). Per target platform.
FROM ghcr.io/protomaps/go-pmtiles:v1.31.2@sha256:06574f01f55a78f78f887bc7ebf729a5c093c0d6e17d9876300cfcb0758b59d3 AS pmtiles

# --- Final Stage ---
# Static binary, no shell, no package manager. :nonroot runs as uid 65532.
FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /app
COPY --from=build /out/gymrouter /app/gymrouter
COPY --from=pmtiles /go-pmtiles /app/pmtiles
ENV PATH=/app:/usr/local/bin:/usr/bin:/bin
# An empty data directory owned by the runtime user, so a fresh named volume mounted
# here inherits that ownership.
COPY --from=build --chown=65532:65532 /out/data /data

USER nonroot
EXPOSE 8080
# Exec form: there's no shell. The binary probes its own /healthz.
# First start downloads ~300 MB of timetables, hence the long start period. The street network (~60 MB)
# and the basemap are fetched in the background afterwards.
HEALTHCHECK --interval=30s --timeout=10s --start-period=300s --retries=3 \
  CMD ["/app/gymrouter", "healthcheck"]
ENTRYPOINT ["/app/gymrouter"]
CMD ["serve"]
