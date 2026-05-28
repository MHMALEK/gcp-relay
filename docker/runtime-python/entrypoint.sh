#!/bin/sh
# Runs a user's Python Cloud Function locally via the Functions Framework.
# The function source is mounted at /workspace. Configuration comes from env:
#   FUNCTION_TARGET          - entry point function name (required)
#   FUNCTION_SIGNATURE_TYPE  - cloudevent (default) | http
#   FUNCTION_SOURCE          - source file (default main.py)
set -e
cd /workspace

if [ -f requirements.txt ]; then
  echo "gcp-relay: installing requirements.txt..."
  pip install --no-cache-dir -r requirements.txt
fi

if [ -z "${FUNCTION_TARGET}" ]; then
  echo "gcp-relay: FUNCTION_TARGET is required" >&2
  exit 1
fi

exec functions-framework \
  --target="${FUNCTION_TARGET}" \
  --signature-type="${FUNCTION_SIGNATURE_TYPE:-cloudevent}" \
  --source="${FUNCTION_SOURCE:-main.py}" \
  --host=0.0.0.0 \
  --port=8080
