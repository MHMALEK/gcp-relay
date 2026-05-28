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

The config mirrors **real GCP resources** — buckets, Pub/Sub topics/subscriptions,
GCS bucket notifications, and Cloud Functions — so it maps 1:1 to what you'd
declare with `gsutil notification create` and `gcloud functions deploy`:

```yaml
version: v2
project_id: local-project

buckets:
  - name: demo-bucket
    versioning: true

# GCS → Pub/Sub (mirrors `gsutil notification create`)
notifications:
  - bucket: demo-bucket
    topic: gcs-notifications
    event_types: [OBJECT_FINALIZE, OBJECT_DELETE]
    object_name_prefix: uploads/
    payload_format: JSON_API_V1

# GCS / Pub/Sub → Cloud Function (mirrors `gcloud functions deploy`)
functions:
  - name: echo-function
    url: http://echo-function:8080      # already-running target; use `source:` to have gcp-relay run it
    trigger:
      event_filters:
        type: google.cloud.storage.object.v1.finalized
        bucket: demo-bucket
```

How it routes: fake-gcs publishes **every** bucket's object events to one
firehose topic; the relay acts as local Eventarc, delivering a faithful
`google.cloud.storage.object.v1.*` CloudEvent to each function whose
`event_filters` match, and republishing to any matching notification topic.

**Versioning:** `version` is optional and auto-detected (legacy `triggers:` ⇒
`v1`, otherwise `v2`). Old `v1` trigger configs still load and are normalized
internally. Unknown versions are rejected.

### Function triggers

| `trigger` | Fires on |
|-----------|----------|
| `event_filters: {type, bucket, object_name_prefix}` | a GCS object event (Eventarc-style) |
| `topic: <name>` | a Pub/Sub message on that topic (delivered as `messagePublished`) |
| `http: true` | a plain HTTP request |

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
- [ ] **Terraform support** (future release): apply real `google`-provider resources
      (`google_storage_bucket`, `google_pubsub_topic`/`_subscription`,
      `google_storage_notification`) against the local emulators via custom endpoints.
- [ ] **Terraform for functions** (stretch): a Cloud Functions Admin API shim so
      `google_cloudfunctions2_function` can deploy to the local stack — true
      "`terraform apply` runs the whole pipeline locally."

## License

MIT
