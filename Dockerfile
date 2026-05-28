FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gcp-relay ./cmd/gcp-relay

FROM alpine:3.20
# docker-cli is included so the relay can launch function containers via a
# mounted /var/run/docker.sock when GCP_RELAY_LAUNCH_FUNCTIONS=true.
RUN apk add --no-cache ca-certificates curl docker-cli
WORKDIR /app
COPY --from=builder /gcp-relay /usr/local/bin/gcp-relay
EXPOSE 8099
ENTRYPOINT ["gcp-relay", "serve"]
# Default CMD intentionally omits --config so the GCP_RELAY_CONFIG env var
# (main.go's flag default) wins for downstream consumers that mount their
# own config. Override with `command: [...]` in compose if you need to
# pass flags explicitly.
CMD ["--port", "8099"]
