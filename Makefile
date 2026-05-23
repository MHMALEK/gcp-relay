.PHONY: build test run tidy up down demo init

build:
	go build -o bin/gcp-relay ./cmd/gcp-relay

test:
	go test ./...

tidy:
	go mod tidy

run:
	go run ./cmd/gcp-relay --config config/triggers.example.yaml --port 8099

up:
	docker compose up --build

down:
	docker compose down

init:
	chmod +x scripts/*.sh
	./scripts/init-pubsub.sh

demo: init
	./scripts/demo-upload.sh
