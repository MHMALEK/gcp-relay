#!/bin/sh
# Runs a user's Go Cloud Function locally via the Functions Framework.
#
# Convention: /workspace is a Go module (has go.mod) whose package registers
# the function in init() via:
#   functions.CloudEvent("name", Handler)
# from github.com/GoogleCloudPlatform/functions-framework-go/functions.
#
# We generate a tiny cmd/gcp-relay-runner/main.go that imports the user's
# package for its init() side effect and starts funcframework.
#
# Configuration:
#   FUNCTION_TARGET  - registered function name (required)
set -e
cd /workspace

if [ ! -f go.mod ]; then
  echo "gcp-relay: /workspace/go.mod not found; the Go function source must be a Go module" >&2
  exit 1
fi
if [ -z "${FUNCTION_TARGET}" ]; then
  echo "gcp-relay: FUNCTION_TARGET is required" >&2
  exit 1
fi

MODULE=$(awk '/^module / {print $2; exit}' go.mod)
if [ -z "$MODULE" ]; then
  echo "gcp-relay: could not parse module path from go.mod" >&2
  exit 1
fi

mkdir -p cmd/gcp-relay-runner
cat > cmd/gcp-relay-runner/main.go <<EOF
package main

import (
	"log"
	"os"

	_ "${MODULE}"
	"github.com/GoogleCloudPlatform/functions-framework-go/funcframework"
)

func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	if err := funcframework.Start(port); err != nil {
		log.Fatalf("funcframework.Start: %v", err)
	}
}
EOF

echo "gcp-relay: go mod tidy (first run can take ~30s)..."
go mod tidy

echo "gcp-relay: starting function..."
exec go run ./cmd/gcp-relay-runner
