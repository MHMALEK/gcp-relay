FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gcp-relay ./cmd/gcp-relay

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
WORKDIR /app
COPY --from=builder /gcp-relay /usr/local/bin/gcp-relay
EXPOSE 8099
ENTRYPOINT ["gcp-relay"]
CMD ["--config", "/config/triggers.example.yaml", "--port", "8099"]
