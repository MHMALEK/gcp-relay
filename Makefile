.PHONY: build test run tidy up down demo init install

build:
	go build -o bin/gcp-relay ./cmd/gcp-relay

install:
	go install ./cmd/gcp-relay

test:
	go test ./...

tidy:
	go mod tidy

run:
	go run ./cmd/gcp-relay serve --config config/triggers.example.yaml --port 8099

up:
	go run ./cmd/gcp-relay up --build

down:
	go run ./cmd/gcp-relay down

init:
	go run ./cmd/gcp-relay init

demo:
	go run ./cmd/gcp-relay demo
