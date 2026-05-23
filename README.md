# gcp-relay

**Local GCP event pipeline emulator.** Relay GCS object notifications and Pub/Sub messages to local Cloud Function targets as CloudEvents — without touching production Eventarc or deployed functions.

Compose [fake-gcs-server](https://github.com/fsouza/fake-gcs-server), the official Pub/Sub emulator, and a small event router into one dev stack.

## Why gcp-relay?

Google ships fragmented emulators (Pub/Sub, Firestore, …) but not a unified **upload → notification → function** path. gcp-relay fills that gap:

```
GCS upload → Pub/Sub topic → gcp-relay → local Cloud Function (Functions Framework)
```

## Quick start

**Prerequisites:** Docker, Docker Compose

```bash
cd gcp-relay
docker compose up --build
```

In another terminal, bootstrap topics and upload a test object:

```bash
./scripts/init-pubsub.sh
./scripts/demo-upload.sh
```

Watch relay logs — you should see a CloudEvent delivered to the example echo function.

## Architecture

| Service | Port | Role |
|---------|------|------|
| `gcs` | 4443 | fake-gcs-server (GCS API + object notifications) |
| `pubsub` | 8085 | Official Pub/Sub emulator |
| `relay` | 8099 | Event router (this project) |
| `echo-function` | 8080 | Example Functions Framework target |

## Configuration

Copy and edit triggers:

```bash
cp config/triggers.example.yaml config/triggers.yaml
```

```yaml
project_id: local-project

# Pub/Sub push subscriptions are created by scripts/init-pubsub.sh
# using the topic names below.

triggers:
  - name: gcs-object-finalize
    source: pubsub
    topic: gcs-notifications
    filters:
      event_type: google.cloud.storage.object.v1.finalized
    targets:
      - url: http://echo-function:8080
        method: POST
```

### Manual GCS event (bypass Pub/Sub)

```bash
curl -s -X POST http://localhost:8099/events/gcs \
  -H 'Content-Type: application/json' \
  -d '{"bucket":"demo-bucket","name":"uploads/hello.txt"}'
```

## SDK / client setup

Point your app at the local stack:

```bash
export STORAGE_EMULATOR_HOST=http://localhost:4443
export PUBSUB_EMULATOR_HOST=localhost:8085
export GCP_RELAY_URL=http://localhost:8099
```

## CLI (native)

Run the relay outside Docker (Pub/Sub + GCS still in Compose):

```bash
go run ./cmd/gcp-relay --config config/triggers.example.yaml --port 8099
```

## Roadmap

- [ ] Eventarc-compatible trigger API (CEL filters)
- [ ] Web UI: event inspector + replay
- [ ] Multi-target fan-out and ordering
- [ ] Composer / Airflow DAG trigger adapter
- [ ] `gcp-relay up` single-binary orchestration (no Compose required)

## License

MIT
