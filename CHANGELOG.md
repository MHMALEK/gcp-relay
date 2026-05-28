# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`DockerLauncher`**: the relay can now launch function containers
  itself via a mounted `/var/run/docker.sock` when
  `GCP_RELAY_LAUNCH_FUNCTIONS=true`. Enables a pure
  `docker compose up` workflow with no `gcp-relay` CLI binary required.
  Wired into [`deploy/docker-compose.yml`](deploy/docker-compose.yml).
- `docker-cli` shipped in the relay image to support the above.
- **End-to-end CI workflow** that builds the relay + pubsub + python
  runner images, brings up the full stack, uploads a real file, and
  verifies the python function receives the CloudEvent. Runs on every
  push to master, on every `v*` tag, nightly, and on manual dispatch
  (not on PRs — keeps PR CI fast).

## [0.1.0] - 2026-05-28

The first release of the GCP-faithful local Eventarc + Cloud Functions runner.

### Added

- **GCP-faithful v2 config schema** mirroring real GCP resources: `buckets`,
  `pubsub` (topics/subscriptions), `notifications` (matches
  `gsutil notification create`), and `functions` (matches
  `gcloud functions deploy`).
- **Local Eventarc routing engine**: the relay receives a single firehose
  topic carrying every bucket's object events and delivers faithful
  `google.cloud.storage.object.v1.*` CloudEvents (in binary mode, matching
  real Eventarc) to functions whose `event_filters` match.
- **GCS notification republishing**: for every matching `notification` rule
  the relay publishes the GCS object payload to the configured Pub/Sub
  topic, so user-owned push subscribers see the same messages real GCS
  would deliver.
- **Pub/Sub topic-triggered functions**: pushes on non-firehose topics are
  wrapped as `google.cloud.pubsub.topic.v1.messagePublished` CloudEvents
  and delivered to topic-triggered functions.
- **Functions Framework runners** for Python (`python312`), Node (`nodejs20`),
  and Go (`go122`). Each runs the user's source directly from a mounted
  volume via the real Functions Framework — no production-code changes.
- **Config-driven CLI**: `up`, `down`, `validate`, `plan`, `logs`, `init`,
  `demo`, `version`. `up` generates a docker-compose from the config and
  runs it; `plan` prints what it would create.
- **Auto-bootstrap mode** (`GCP_RELAY_AUTO_BOOTSTRAP=true`) so the relay
  can self-bootstrap from its config when launched outside the CLI flow
  (e.g. from the standalone compose).
- **Canonical hostnames + cross-stack network**: a fixed `gcp-relay` Docker
  network with aliases `gcs.localhost`, `storage.gcp.localhost`,
  `pubsub.localhost`, `relay.localhost`. Apps in another compose attach
  with `networks: { gcp-relay: { external: true } }` and reach the
  emulators by name with no host changes.
- **Configurable host ports** (`GCP_RELAY_HOST_PUBSUB_PORT`,
  `GCP_RELAY_HOST_GCS_PORT`, `GCP_RELAY_HOST_RELAY_PORT`) so the stack can
  coexist with other local containers that already hold the defaults.
- **Standalone emulators+relay compose** at `deploy/docker-compose.yml`
  for users who want to `include:` gcp-relay in their own compose project.
- **Multi-platform binary releases** via `goreleaser` on every `v*` tag.
- **Five GHCR images** published in parallel on every default-branch push
  and on every `v*` tag: `gcp-relay`, `gcp-relay-pubsub`,
  `gcp-relay-runtime-{python,node,go}`.
- **CI** with gofmt + vet + race-tested test runs.
- **Inspector UI** + `/events`, `/events/{id}`, `/events/{id}/replay`,
  and `/admin/bootstrap` HTTP APIs.

### Notes

- The relay delivers CloudEvents in pure HTTP **binary content mode**
  (Ce-\* headers + body is the `data` payload), matching how production
  Eventarc delivers to Cloud Run / Cloud Functions 2nd gen. Mixing modes
  confuses Node's Functions Framework.
- Backward compatibility with any legacy schema is intentionally **not**
  preserved — this is a greenfield project.

[Unreleased]: https://github.com/MHMALEK/gcp-relay/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/MHMALEK/gcp-relay/releases/tag/v0.1.0
