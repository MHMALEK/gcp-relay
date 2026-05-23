#!/usr/bin/env bash
set -euo pipefail
exec "$(cd "$(dirname "$0")/.." && pwd)/bin/gcp-relay" init 2>/dev/null || go run ./cmd/gcp-relay init
