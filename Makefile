.PHONY: build run run-warp test clean install deps setup warp-setup warp-connect warp-status

build:
	go build -o bin/signal-bot cmd/bot/main.go

run:
	go run cmd/bot/main.go -config configs/config.yaml

# Run bot through Cloudflare WARP proxy (when your IP is blocked by IQ Option)
# First time: make warp-setup
# Each time:  make warp-connect && make run-warp
run-warp:
	HTTPS_PROXY=http://127.0.0.1:8118 go run cmd/bot/main.go -config configs/config.yaml

# First-time WARP setup (run once)
warp-setup:
	@echo "Installing Cloudflare WARP + privoxy..."
	curl -fsSL https://pkg.cloudflareclient.com/pubkey.gpg | sudo gpg --yes --dearmor --output /usr/share/keyrings/cloudflare-warp-archive-keyring.gpg
	echo "deb [signed-by=/usr/share/keyrings/cloudflare-warp-archive-keyring.gpg] https://pkg.cloudflareclient.com/ $$(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/cloudflare-client.list
	sudo apt update -qq && sudo apt install cloudflare-warp privoxy -y
	warp-cli registration new
	warp-cli mode proxy
	echo "forward-socks5 / 127.0.0.1:40000 ." | sudo tee -a /etc/privoxy/config
	sudo systemctl restart privoxy
	@echo "✓ Done. Now run: make warp-connect && make run-warp"

# Connect to WARP
warp-connect:
	warp-cli connect
	@sleep 2
	@warp-cli status

# Check status
warp-status:
	@warp-cli status 2>/dev/null || echo "WARP not installed"
	@echo "Current IP via proxy: $$(HTTPS_PROXY=http://127.0.0.1:8118 curl -s --max-time 5 https://api.ipify.org 2>/dev/null || echo 'proxy not running')"

test:
	go test -v ./...

test-parser:
	go test -v ./internal/parser/...

test-mexy:
	go run cmd/test-parser/main.go

clean:
	rm -rf bin/ logs/ data/ session/

install:
	go mod download
	go mod tidy

setup: install
	cp configs/config.example.yaml configs/config.yaml
	@echo "Edit configs/config.yaml with your credentials"

docker-build:
	docker build -t signal-bot .

docker-run:
	docker run -v $(PWD)/configs:/app/configs -v $(PWD)/data:/app/data signal-bot
