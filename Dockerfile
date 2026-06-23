FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git build-base

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -o signal-bot cmd/bot/main.go

FROM alpine:latest

RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    freetype-dev \
    harfbuzz \
    ca-certificates \
    ttf-freefont \
    sqlite

ENV CHROME_BIN=/usr/bin/chromium-browser
ENV CHROME_PATH=/usr/lib/chromium/

WORKDIR /app

COPY --from=builder /app/signal-bot .

RUN mkdir -p /app/configs /app/data /app/logs /app/session

VOLUME ["/app/configs", "/app/data", "/app/logs", "/app/session"]

CMD ["./signal-bot", "-config", "/app/configs/config.yaml"]
