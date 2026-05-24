# gcp-relay

**Local GCP event pipeline emulator.** Relay GCS object notifications and Pub/Sub messages to local Cloud Function targets as CloudEvents — without touching production Eventarc or deployed functions.

Compose [fake-gcs-server](https://github.com/fsouza/fake-gcs-server), the Pub/Sub emulator, and a small event router into one dev stack.

## Why gcp-relay?

Google ships fragmented emulators (Pub/Sub, Firestore, …) but not a unified **upload → notification → function** path. gcp-relay fills that gap:

```
GCS upload → Pub/Sub topic → gcp-relay → local Cloud Function (Functions Framework)
```

## Quick start

**Prerequisites:** Docker, Docker Compose, Go 1.22+ (optional, for native CLI)

### One command

```bash
git clone git@github.com:MHMALEK/gcp-relay.git
cd gcp-relay
go run ./cmd/gcp-relay up --build
go run ./cmd/gcp-relay demo
```

Open the inspector: **http://localhost:8099/ui/**

### Manual

```bash
docker compose up --build -d
go run ./cmd/gcp-relay init
go run ./cmd/gcp-relay demo
```

## CLI

| Command | Description |
|---------|-------------|
| `gcp-relay up [--build]` | Start stack + bootstrap Pub/Sub/GCS |
| `gcp-relay down` | Stop stack |
| `gcp-relay init` | Create topic, push subscription, bucket notification |
| `gcp-relay demo` | Upload demo file to local GCS |
| `gcp-relay serve` | Run relay only (native) |

Install locally:

```bash
go install ./cmd/gcp-relay
gcp-relay up --build
```

## Architecture

| Service | Port | Role |
|---------|------|------|
| `gcs` | 4443 | fake-gcs-server (GCS API + object notifications) |
| `pubsub` | 8085 | Pub/Sub emulator (built from `docker/pubsub`) |
| `relay` | 8099 | Event router + inspector UI |
| `echo-function` | 8080 | Example Functions Framework target |

## Container images

Prebuilt images are published to GHCR on every push to the default branch and on every `v*` tag:

| Image | Purpose |
|-------|---------|
| `ghcr.io/mhmalek/gcp-relay` | The relay binary (entrypoint `gcp-relay serve`) |
| `ghcr.io/mhmalek/gcp-relay-pubsub` | Pub/Sub emulator container used in the compose stack |

Tags: `:main` (rolling default branch), `:sha-<short>`, `:vX.Y.Z`, `:vX.Y`, `:latest` (latest tagged release).

Consumers should pin to a `:vX.Y.Z` tag.

## Configuration

```bash
cp config/triggers.example.yaml config/triggers.yaml
```

The `triggers.yaml` schema is the **stable public contract** for downstream consumers. It is versioned:

```yaml
version: v1            # optional; defaults to v1 for back-compat. Unknown versions are rejected.
project_id: local-project

triggers:
  - name: gcs-object-finalize
    source: pubsub
    topic: gcs-notifications
    filters:
      event_type: google.cloud.storage.object.v1.finalized
      object_prefix: uploads/
    targets:
      - type: cloudevent
        url: http://echo-function:8080
```

Breaking changes to the schema bump the version (e.g. `v2`). Older versions remain supported.

### Target types

| `type` | Delivers |
|--------|----------|
| `cloudevent` (default) | CloudEvents JSON + `Ce-*` headers to a Functions Framework URL |
| `http` | Same as `cloudevent` — raw HTTP POST with CloudEvent payload |

### Manual GCS event (bypass Pub/Sub)

```bash
curl -s -X POST http://localhost:8099/events/gcs \
  -H 'Content-Type: application/json' \
  -d '{"bucket":"demo-bucket","name":"uploads/hello.txt"}'
```

### Remote bootstrap (`POST /admin/bootstrap`)

Programmatic equivalent of `gcp-relay init` — lets downstream consumers (e.g. tract-cli) drive bootstrap over HTTP without touching the Pub/Sub or fake-gcs REST APIs directly.

```bash
curl -s -X POST http://localhost:8099/admin/bootstrap \
  -H 'Content-Type: application/json' \
  -d '{
    "project_id": "local-project",
    "topic":      "gcs-notifications",
    "bucket":     "my-data-pipeline",
    "push_url":   "http://gcp-relay:8099"
  }'
```

All fields are optional and fall back to the relay's defaults (`GCP_RELAY_PROJECT`, `GCP_RELAY_GCS_TOPIC`, `GCP_RELAY_DEMO_BUCKET`, `GCP_RELAY_PUSH_URL`, `PUBSUB_EMULATOR_HOST`, `STORAGE_EMULATOR_HOST`). When the relay runs inside a docker network with its Pub/Sub neighbour, set `push_url` to the relay's docker service name (e.g. `http://gcp-relay:8099`), not `host.docker.internal`.

## Event inspector

- **UI:** http://localhost:8099/ui/
- **API:** `GET /events`, `GET /events/{id}`, `POST /events/{id}/replay`

## SDK / client setup

```bash
export STORAGE_EMULATOR_HOST=http://localhost:4443
export PUBSUB_EMULATOR_HOST=localhost:8085
export GCP_RELAY_URL=http://localhost:8099
# Pub/Sub emulator runs in Docker — push subscriptions must reach the host relay:
export GCP_RELAY_PUSH_URL=http://host.docker.internal:8099
```

## Roadmap

- [x] `gcp-relay up` orchestration
- [x] Event inspector UI + replay API
- [x] Object prefix filters
- [ ] Eventarc-compatible trigger CRUD API
- [ ] Pub/Sub emulator wiring for non-GCS functions
- [ ] Single static binary bundling emulators (no Docker)

## License

MIT
