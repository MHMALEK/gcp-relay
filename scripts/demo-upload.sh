#!/usr/bin/env bash
set -euo pipefail

GCS_HOST="${STORAGE_EMULATOR_HOST:-http://localhost:4443}"
BUCKET="${GCP_RELAY_DEMO_BUCKET:-demo-bucket}"
OBJECT="${GCP_RELAY_DEMO_OBJECT:-uploads/hello.txt}"
RELAY_URL="${GCP_RELAY_URL:-http://localhost:8099}"

echo "==> Uploading ${OBJECT} to gs://${BUCKET}/"
curl -s -X POST \
  "${GCS_HOST}/upload/storage/v1/b/${BUCKET}/o?uploadType=media&name=${OBJECT}" \
  -H 'Content-Type: text/plain' \
  -d 'hello from gcp-relay demo'

echo
echo "==> If Pub/Sub push is configured, check relay + echo-function logs."
echo "==> Or trigger relay directly:"
curl -s -X POST "${RELAY_URL}/events/gcs" \
  -H 'Content-Type: application/json' \
  -d "{\"bucket\":\"${BUCKET}\",\"name\":\"${OBJECT}\"}"
echo
