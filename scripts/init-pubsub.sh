#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROJECT_ID="${GCP_RELAY_PROJECT:-local-project}"
PUBSUB_HOST="${PUBSUB_EMULATOR_HOST:-localhost:8085}"
GCS_HOST="${STORAGE_EMULATOR_HOST:-http://localhost:4443}"
TOPIC="${GCP_RELAY_GCS_TOPIC:-gcs-notifications}"
BUCKET="${GCP_RELAY_DEMO_BUCKET:-demo-bucket}"
RELAY_URL="${GCP_RELAY_URL:-http://localhost:8099}"

echo "==> Creating Pub/Sub topic: ${TOPIC}"
curl -s -X PUT "http://${PUBSUB_HOST}/v1/projects/${PROJECT_ID}/topics/${TOPIC}" \
  -H 'Content-Type: application/json' \
  -d '{}' >/dev/null || true

echo "==> Creating push subscription to relay"
curl -s -X PUT "http://${PUBSUB_HOST}/v1/projects/${PROJECT_ID}/subscriptions/gcs-relay-push" \
  -H 'Content-Type: application/json' \
  -d "{
    \"topic\": \"projects/${PROJECT_ID}/topics/${TOPIC}\",
    \"pushConfig\": {
      \"pushEndpoint\": \"${RELAY_URL}/events/pubsub/${TOPIC}\"
    }
  }" >/dev/null || true

echo "==> Creating GCS bucket: ${BUCKET}"
curl -s -X POST "${GCS_HOST}/storage/v1/b" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"${BUCKET}\"}" >/dev/null || true

echo "==> Adding GCS notification config (OBJECT_FINALIZE -> Pub/Sub)"
curl -s -X POST "${GCS_HOST}/storage/v1/b/${BUCKET}/notificationConfigs" \
  -H 'Content-Type: application/json' \
  -d "{
    \"topic\": \"projects/${PROJECT_ID}/topics/${TOPIC}\",
    \"payload_format\": \"JSON_API_V1\",
    \"event_types\": [\"OBJECT_FINALIZE\"]
  }" >/dev/null || true

echo "Done. Topic=${TOPIC} bucket=${BUCKET}"
