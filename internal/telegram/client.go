package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"

	"signal-bot/internal/config"
)

type Client struct {
	cfg     *config.TelegramConfig
	client  *telegram.Client
	api     *tg.Client
	logger  zerolog.Logger
	handler MessageHandler
}

type MessageHandler func(ctx context.Context, message string) error

func New(cfg *config.TelegramConfig, logger zerolog.Logger) *Client {
	return &Client{cfg: cfg, logger: logger}
}

func (c *Client) ConnectAndListen(ctx context.Context, handler MessageHandler) error {
	c.logger.Info().Msg("initializing telegram connection...")

	if err := os.MkdirAll(filepath.Dir(c.cfg.SessionFile), 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	c.handler = handler
	backoff := []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}

	for {
		c.client = telegram.NewClient(c.cfg.ApiID, c.cfg.ApiHash, telegram.Options{
			SessionStorage: &telegram.FileSessionStorage{Path: c.cfg.SessionFile},
		})

		err := c.client.Run(ctx, func(ctx context.Context) error {
			c.api = c.client.API()

			c.logger.Info().Msg("checking authentication status...")
			status, err := c.client.Auth().Status(ctx)
			if err != nil {
				return fmt.Errorf("get auth status: %w", err)
			}
			if !status.Authorized {
				if err := c.authenticate(ctx); err != nil {
					return fmt.Errorf("authenticate: %w", err)
				}
			} else {
				c.logger.Info().Msg("already authorized (using saved session)")
			}

			c.logger.Info().Msg("✓ telegram client connected and ready")
			return c.pollMessages(ctx)
		})

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			c.logger.Error().Err(err).Msg("telegram disconnected")
		}

		idx := 0
		for i := range backoff {
			if i < len(backoff)-1 {
				idx = i
			}
		}
		delay := backoff[idx]
		c.logger.Info().Dur("wait", delay).Msg("🔄 Reconnecting to Telegram...")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (c *Client) pollMessages(ctx context.Context) error {
	channelID := c.cfg.ChannelID
	if channelID < 0 {
		channelID = -(channelID + 1000000000000)
	}

	c.logger.Info().Msg("resolving channel to get access hash...")

	dialogs, err := c.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	if err != nil {
		return fmt.Errorf("failed to get dialogs: %w", err)
	}

	var accessHash int64
	var found bool

	for _, chat := range extractChats(dialogs) {
		if ch, ok := chat.(*tg.Channel); ok && ch.ID == channelID {
			accessHash = ch.AccessHash
			found = true
			c.logger.Info().Str("title", ch.Title).Int64("channel_id", channelID).Msg("✓ found target channel with access hash")
			break
		}
	}
	if !found {
		return fmt.Errorf("channel %d not found in dialogs", channelID)
	}

	lastMessageID := 0
	firstPoll := true
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	c.logger.Info().Int64("channel_id", channelID).Msg("✓ starting message polling loop")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			history, err := c.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
				Peer:  &tg.InputPeerChannel{ChannelID: channelID, AccessHash: accessHash},
				Limit: 10,
			})
			if err != nil {
				c.logger.Debug().Err(err).Msg("poll error (will retry)")
				continue
			}

			var messages []*tg.Message
			if h, ok := history.(*tg.MessagesChannelMessages); ok {
				for _, msg := range h.Messages {
					if m, ok := msg.(*tg.Message); ok {
						messages = append(messages, m)
					}
				}
			}

			if firstPoll {
				for _, msg := range messages {
					if msg.ID > lastMessageID {
						lastMessageID = msg.ID
					}
				}
				firstPoll = false
				c.logger.Info().Int("last_msg_id", lastMessageID).Msg("✅ bot ready - watching for NEW messages only (existing messages ignored)")
				continue
			}

			for _, msg := range messages {
				if msg.ID > lastMessageID {
					lastMessageID = msg.ID
					preview := msg.Message
					if len(preview) > 50 {
						preview = preview[:50] + "..."
					}
					c.logger.Info().Int("msg_id", msg.ID).Str("preview", preview).Msg("📨 NEW MESSAGE DETECTED")
					if err := c.handler(ctx, msg.Message); err != nil {
						c.logger.Error().Err(err).Msg("failed to handle message")
					}
				}
			}
		}
	}
}

func extractChats(dialogs tg.MessagesDialogsClass) []tg.ChatClass {
	switch d := dialogs.(type) {
	case *tg.MessagesDialogs:
		return d.Chats
	case *tg.MessagesDialogsSlice:
		return d.Chats
	}
	return nil
}

func (c *Client) authenticate(ctx context.Context) error {
	c.logger.Info().Msg("TELEGRAM AUTHENTICATION REQUIRED")
	flow := auth.NewFlow(terminalAuth{phone: c.cfg.Phone, logger: c.logger}, auth.SendCodeOptions{})
	return c.client.Auth().IfNecessary(ctx, flow)
}

type terminalAuth struct {
	phone  string
	logger zerolog.Logger
}

func (a terminalAuth) Phone(_ context.Context) (string, error) {
	return a.phone, nil
}

func (a terminalAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Enter 2FA password: ")
	var pwd string
	fmt.Scanln(&pwd)
	return pwd, nil
}

func (a terminalAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	a.logger.Info().Msg("📱 Check your Telegram app or phone for the verification code")
	fmt.Print("Enter verification code: ")
	var code string
	fmt.Scanln(&code)
	if code == "" {
		return "", fmt.Errorf("no code entered")
	}
	return code, nil
}

func (a terminalAuth) AcceptTermsOfService(_ context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

func (a terminalAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("signup not supported")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
