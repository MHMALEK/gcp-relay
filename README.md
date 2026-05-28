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
| `gcp-relay up [--config path] [--build]` | Generate compose, start stack, bootstrap |
| `gcp-relay down [--config path]` | Stop the generated stack |
| `gcp-relay validate [--config path]` | Validate the config (incl. function sources) |
| `gcp-relay init [--config path]` | Bootstrap against an already-running stack |
| `gcp-relay demo` | Upload a demo file to local GCS |
| `gcp-relay serve` | Run the relay only (native) |
| `gcp-relay version` | Print the version |

### Install

Download a prebuilt binary from the [GitHub Releases](https://github.com/MHMALEK/gcp-relay/releases) (linux/macOS, amd64/arm64), or:

```bash
go install github.com/MHMALEK/gcp-relay/cmd/gcp-relay@latest
gcp-relay up
```

### Host port overrides

If the default ports clash with other local containers, override them:

```bash
export GCP_RELAY_HOST_PUBSUB_PORT=18085
export GCP_RELAY_HOST_GCS_PORT=14443
export GCP_RELAY_HOST_RELAY_PORT=18099
gcp-relay up
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
| `ghcr.io/mhmalek/gcp-relay-runtime-python` | Python Functions Framework runner |
| `ghcr.io/mhmalek/gcp-relay-runtime-node` | Node.js Functions Framework runner |

Tags: `:main` (rolling default branch), `:sha-<short>`, `:vX.Y.Z`, `:vX.Y`, `:latest` (latest tagged release).

Consumers should pin to a `:vX.Y.Z` tag.

### Releasing

Releases are cut by pushing a semver tag:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

That triggers two parallel workflows: [`release.yml`](.github/workflows/release.yml) runs `goreleaser` to publish multi-platform binaries + a GitHub release, and [`publish-images.yml`](.github/workflows/publish-images.yml) builds the four container images to GHCR.

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

## Pointing your app at the emulators

A presets file lives at [`deploy/env.emulator`](deploy/env.emulator) — source
it (or pass the same vars to `docker run -e ...`) and any GCP client library
talks to the local stack:

```bash
set -a && . deploy/env.emulator && set +a
# STORAGE_EMULATOR_HOST=http://gcs.localhost:4443
# PUBSUB_EMULATOR_HOST=pubsub.localhost:8085
# GOOGLE_APPLICATION_CREDENTIALS=.../deploy/fake-adc.json
```

The canonical hostnames `gcs.localhost` / `pubsub.localhost` resolve via the
Docker network alias for any container on the `gcp-relay` network. For
apps running directly on your host, either:

- use `localhost:<published port>` (works with no setup), or
- add a one-time line to `/etc/hosts`:
  `127.0.0.1  gcs.localhost pubsub.localhost`

### Per-language auth

The env var honoring is consistent across Go/Python; Node and Java need an
extra knob:

| Client | What's needed | Code change |
|---|---|---|
| **Go** (`cloud.google.com/go/storage`) | `STORAGE_EMULATOR_HOST` only — auto-reroutes and skips auth | none |
| **Python** (`google-cloud-storage`) | `STORAGE_EMULATOR_HOST` only — auto-reroutes + anonymous creds | none |
| **Node** (`@google-cloud/storage`) | env var **+** `new Storage({ apiEndpoint, useAuthWithCustomEndpoint: false })` | one option |
| **Java** | `StorageOptions.newBuilder().setHost("http://gcs.localhost:4443").setCredentials(NoCredentials.getInstance()).build()` | a few lines |

`deploy/fake-adc.json` is a valid-shape but worthless ADC JSON so clients
that still fall through Application Default Credentials get a parseable
file instead of hitting Google's metadata server.

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
