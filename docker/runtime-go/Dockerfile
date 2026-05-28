FROM golang:1.22-alpine

# /go/pkg/mod is the module cache; preserve across container restarts when
# users mount a volume here to skip re-downloading on every start.
ENV GOPATH=/go GOMODCACHE=/go/pkg/mod CGO_ENABLED=0
RUN mkdir -p /go/pkg/mod && apk add --no-cache bash

WORKDIR /workspace
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
