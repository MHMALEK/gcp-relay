#!/bin/sh
# Runs a user's Node.js Cloud Function locally via the Functions Framework.
# The function source is mounted at /workspace. Configuration comes from env:
#   FUNCTION_TARGET          - registered function name (required)
#   FUNCTION_SIGNATURE_TYPE  - cloudevent (default) | http
#   FUNCTION_SOURCE          - source file (default index.js)
set -e
cd /workspace

if [ -f package.json ]; then
  echo "gcp-relay: npm install (first run can take ~30s)..."
  npm install --omit=dev --silent
elif [ ! -d node_modules/@google-cloud/functions-framework ]; then
  echo "gcp-relay: installing functions-framework (no package.json found)..."
  npm install --omit=dev --silent --no-save @google-cloud/functions-framework@3
fi

if [ -z "${FUNCTION_TARGET}" ]; then
  echo "gcp-relay: FUNCTION_TARGET is required" >&2
  exit 1
fi

exec npx --no-install functions-framework \
  --target="${FUNCTION_TARGET}" \
  --signature-type="${FUNCTION_SIGNATURE_TYPE:-cloudevent}" \
  --source="${FUNCTION_SOURCE:-index.js}" \
  --port=8080
