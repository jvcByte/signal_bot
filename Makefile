.PHONY: build run test clean install deps

build:
	go build -o bin/signal-bot cmd/bot/main.go

run:
	go run cmd/bot/main.go -config configs/config.yaml

test:
	go test -v ./...

test-parser:
	go test -v ./internal/parser/...

test-mexy:
	go run cmd/test-parser/main.go

clean:
	rm -rf bin/
	rm -rf logs/
	rm -rf data/
	rm -rf session/

install:
	go mod download
	go mod tidy

deps:
	go get github.com/gotd/td@latest
	go get github.com/gorilla/websocket@latest
	go get gopkg.in/yaml.v3@latest
	go get github.com/rs/zerolog@latest
	go get github.com/mattn/go-sqlite3@latest
	go get github.com/google/uuid@latest

setup: install
	cp configs/config.example.yaml configs/config.yaml
	@echo "Edit configs/config.yaml with your credentials"

docker-build:
	docker build -t signal-bot .

docker-run:
	docker run -v $(PWD)/configs:/app/configs -v $(PWD)/data:/app/data signal-bot
