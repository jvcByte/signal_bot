.PHONY: build run run-warp test clean install deps warp-setup warp-connect warp-status

build:
	go build -o bin/signal-bot cmd/bot/main.go

run:
	go run cmd/bot/main.go -config configs/config.yaml

# Run bot through Cloudflare WARP proxy (use when your IP is blocked by IQ Option)
# Setup: make warp-setup (first time only)
# Connect: make warp-connect
# Then: make run-warp
run-warp:
	proxychains4 go run cmd/bot/main.go -config configs/config.yaml

# First-time WARP setup
warp-setup:
	@echo "Installing Cloudflare WARP + proxychains..."
	curl -fsSL https://pkg.cloudflareclient.com/pubkey.gpg | sudo gpg --yes --dearmor --output /usr/share/keyrings/cloudflare-warp-archive-keyring.gpg
	echo "deb [signed-by=/usr/share/keyrings/cloudflare-warp-archive-keyring.gpg] https://pkg.cloudflareclient.com/ $$(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/cloudflare-client.list
	sudo apt update -qq && sudo apt install cloudflare-warp proxychains4 -y
	warp-cli registration new
	warp-cli mode proxy
	@echo '[ProxyList]\nsocks5 127.0.0.1 40000' | sudo tee /etc/proxychains4.conf
	@echo "✓ WARP installed. Run 'make warp-connect' then 'make run-warp'."

# Connect to WARP (run before make run-warp)
warp-connect:
	warp-cli connect
	@sleep 2
	@warp-cli status

# Check WARP connection status
warp-status:
	@warp-cli status
	@echo "Proxy port: $$(ss -tlnp | grep 40000 | awk '{print $$4}')"
	@echo "Current IP: $$(HTTPS_PROXY=socks5://127.0.0.1:40000 curl -s --max-time 5 https://api.ipify.org 2>/dev/null || echo 'not connected')"

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
