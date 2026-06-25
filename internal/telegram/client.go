package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/updates"
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
	return &Client{
		cfg:    cfg,
		logger: logger,
	}
}

func (c *Client) ConnectAndListen(ctx context.Context, handler MessageHandler) error {
	c.logger.Info().Msg("initializing telegram connection...")

	if err := os.MkdirAll(filepath.Dir(c.cfg.SessionFile), 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	c.logger.Debug().Str("path", filepath.Dir(c.cfg.SessionFile)).Msg("session directory created")

	c.logger.Info().
		Int("api_id", c.cfg.ApiID).
		Str("session_file", c.cfg.SessionFile).
		Msg("creating telegram client")

	c.client = telegram.NewClient(c.cfg.ApiID, c.cfg.ApiHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: c.cfg.SessionFile,
		},
	})

	c.handler = handler

	c.logger.Info().Msg("connecting to telegram servers...")

	backoff := []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}

	for {
		err := c.client.Run(ctx, func(ctx context.Context) error {
			c.api = c.client.API() // must be set inside Run
			c.logger.Debug().Msg("telegram API client initialized")

			c.logger.Info().Msg("checking authentication status...")
			status, err := c.client.Auth().Status(ctx)
			if err != nil {
				return fmt.Errorf("get auth status: %w", err)
			}

			if !status.Authorized {
				c.logger.Warn().Msg("not authorized, starting authentication flow")
				if err := c.authenticate(ctx); err != nil {
					return fmt.Errorf("authenticate: %w", err)
				}
				c.logger.Info().Msg("authentication successful")
			} else {
				c.logger.Info().Msg("already authorized (using saved session)")
			}

			c.logger.Info().Msg("✓ telegram client connected and ready")
			return c.pollMessages(ctx)
		})

		if ctx.Err() != nil {
			return ctx.Err() // context cancelled - clean shutdown
		}

		if err != nil {
			c.logger.Error().Err(err).Msg("telegram disconnected")
		}

		// Reconnect with backoff
		attempt := 0
		for {
			idx := attempt
			if idx >= len(backoff) {
				idx = len(backoff) - 1
			}
			delay := backoff[idx]
			c.logger.Info().Dur("wait", delay).Msg("🔄 Reconnecting to Telegram...")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}

			// Recreate client for fresh connection
			c.client = telegram.NewClient(c.cfg.ApiID, c.cfg.ApiHash, telegram.Options{
				SessionStorage: &telegram.FileSessionStorage{
					Path: c.cfg.SessionFile,
				},
			})
			break // try again in outer loop
		}
	}
}

func (c *Client) pollMessages(ctx context.Context) error {
	// Convert channel ID for API calls
	channelID := c.cfg.ChannelID
	if channelID < 0 {
		// Remove -100 prefix: -1003488226342 -> 3488226342
		channelID = -(channelID + 1000000000000)
	}
	
	c.logger.Info().Msg("resolving channel to get access hash...")
	
	// First, we need to get the channel's access hash by getting all our dialogs
	// This will give us access to channels we're subscribed to
	dialogs, err := c.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	
	if err != nil {
		return fmt.Errorf("failed to get dialogs: %w", err)
	}
	
	var accessHash int64
	var foundChannel bool
	
	// Extract chats from dialogs
	switch d := dialogs.(type) {
	case *tg.MessagesDialogs:
		c.logger.Debug().Int("chat_count", len(d.Chats)).Msg("searching through chats")
		for _, chat := range d.Chats {
			if ch, ok := chat.(*tg.Channel); ok {
				c.logger.Debug().
					Int64("id", ch.ID).
					Str("title", ch.Title).
					Msg("found channel")
				
				if ch.ID == channelID {
					accessHash = ch.AccessHash
					foundChannel = true
					c.logger.Info().
						Str("title", ch.Title).
						Int64("channel_id", channelID).
						Msg("✓ found target channel with access hash")
					break
				}
			}
		}
	case *tg.MessagesDialogsSlice:
		c.logger.Debug().Int("chat_count", len(d.Chats)).Msg("searching through chats (slice)")
		for _, chat := range d.Chats {
			if ch, ok := chat.(*tg.Channel); ok {
				c.logger.Debug().
					Int64("id", ch.ID).
					Str("title", ch.Title).
					Msg("found channel")
				
				if ch.ID == channelID {
					accessHash = ch.AccessHash
					foundChannel = true
					c.logger.Info().
						Str("title", ch.Title).
						Int64("channel_id", channelID).
						Msg("✓ found target channel with access hash")
					break
				}
			}
		}
	}
	
	if !foundChannel {
		return fmt.Errorf("channel not found in your dialogs - make sure you're subscribed to channel ID %d", channelID)
	}
	
	// Track the last message ID we've seen
	lastMessageID := 0
	pollInterval := 2 * time.Second // Poll every 2 seconds
	firstPoll := true // flag to skip processing on first poll

	c.logger.Info().
		Int64("channel_id", channelID).
		Dur("poll_interval", pollInterval).
		Msg("✓ starting message polling loop")

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info().Msg("polling stopped (context cancelled)")
			return ctx.Err()
		case <-ticker.C:
			// Fetch recent messages with proper access hash
			history, err := c.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
				Peer: &tg.InputPeerChannel{
					ChannelID:  channelID,
					AccessHash: accessHash,
				},
				Limit: 10, // Check last 10 messages
			})

			if err != nil {
				c.logger.Debug().Err(err).Msg("poll error (will retry)")
				continue
			}

			// Extract messages
			var messages []*tg.Message
			switch h := history.(type) {
			case *tg.MessagesChannelMessages:
				for _, msg := range h.Messages {
					if m, ok := msg.(*tg.Message); ok {
						messages = append(messages, m)
					}
				}
			}

			if firstPoll {
				// On first poll: just record the latest message ID, don't process anything
				for _, msg := range messages {
					if msg.ID > lastMessageID {
						lastMessageID = msg.ID
					}
				}
				firstPoll = false
				c.logger.Info().Int("last_msg_id", lastMessageID).Msg("✅ bot ready - watching for NEW messages only (existing messages ignored)")
				c.logger.Info().Msg("📨 Send a NEW message to the channel to trigger a trade")
				continue
			}

			// Process only messages newer than what we've seen
			newMessages := 0
			for _, msg := range messages {
				if msg.ID > lastMessageID {
					newMessages++
					preview := msg.Message
					if len(preview) > 50 {
						preview = preview[:50] + "..."
					}
					c.logger.Info().
						Int("msg_id", msg.ID).
						Str("preview", preview).
						Msg("📨 NEW MESSAGE DETECTED")

					if msg.ID > lastMessageID {
						lastMessageID = msg.ID
					}

					if err := c.handler(ctx, msg.Message); err != nil {
						c.logger.Error().Err(err).Msg("failed to handle message")
					}
				}
			}

			if newMessages > 0 {
				c.logger.Debug().Int("new_messages", newMessages).Msg("processed new messages")
			}
		}
	}
}

func (c *Client) authenticate(ctx context.Context) error {
	c.logger.Info().Msg("══════════════════════════════════════")
	c.logger.Info().Msg("  TELEGRAM AUTHENTICATION REQUIRED")
	c.logger.Info().Msg("══════════════════════════════════════")
	c.logger.Info().Str("phone", c.cfg.Phone).Msg("authenticating with phone number")
	
	flow := auth.NewFlow(
		terminalAuth{phone: c.cfg.Phone, logger: c.logger},
		auth.SendCodeOptions{},
	)

	if err := c.client.Auth().IfNecessary(ctx, flow); err != nil {
		return err
	}

	c.logger.Info().Msg("══════════════════════════════════════")
	c.logger.Info().Msg("  ✓ AUTHENTICATION SUCCESSFUL")
	c.logger.Info().Msg("══════════════════════════════════════")
	return nil
}

func (c *Client) Listen(ctx context.Context, handler MessageHandler) error {
	c.logger.Info().Int64("channel_id", c.cfg.ChannelID).Msg("starting to listen for messages from channel")
	
	// Store handler for use in Handle method
	c.handler = handler
	
	// Get current user ID for updates
	self, err := c.api.UsersGetFullUser(ctx, &tg.InputUserSelf{})
	if err != nil {
		return fmt.Errorf("get self: %w", err)
	}
	
	userID := self.FullUser.ID
	c.logger.Debug().Int64("user_id", userID).Msg("got user ID")
	
	// Create updates manager
	gaps := updates.New(updates.Config{
		Handler: c,
	})
	
	// Start handling updates
	c.logger.Info().Msg("✓ starting update loop...")
	return gaps.Run(ctx, c.api, userID, updates.AuthOptions{})
}

// Handle implements updates.Handler interface
func (c *Client) Handle(ctx context.Context, u tg.UpdatesClass) error {
	c.logger.Debug().Str("update_type", fmt.Sprintf("%T", u)).Msg("📥 received update from Telegram")
	
	// Extract messages from different update types
	var messages []*tg.Message
	
	switch update := u.(type) {
	case *tg.Updates:
		c.logger.Debug().Int("update_count", len(update.Updates)).Msg("processing Updates batch")
		for _, upd := range update.Updates {
			if msg := extractMessage(upd); msg != nil {
				messages = append(messages, msg)
			}
		}
	case *tg.UpdateShortMessage:
		// Direct message, not from channel
		c.logger.Debug().Msg("received UpdateShortMessage (direct message, skipping)")
		return nil
	case *tg.UpdateShort:
		c.logger.Debug().Msg("received UpdateShort")
		if msg := extractMessage(update.Update); msg != nil {
			messages = append(messages, msg)
		}
	default:
		c.logger.Debug().Str("type", fmt.Sprintf("%T", u)).Msg("received unknown update type")
	}
	
	c.logger.Debug().Int("message_count", len(messages)).Msg("extracted messages from update")
	
	// Process each message
	for _, msg := range messages {
		if err := c.handleMessage(ctx, msg); err != nil {
			c.logger.Error().Err(err).Msg("failed to handle message")
		}
	}
	
	return nil
}

func extractMessage(u tg.UpdateClass) *tg.Message {
	switch upd := u.(type) {
	case *tg.UpdateNewMessage:
		if msg, ok := upd.Message.(*tg.Message); ok {
			return msg
		}
	case *tg.UpdateNewChannelMessage:
		if msg, ok := upd.Message.(*tg.Message); ok {
			return msg
		}
	}
	return nil
}

func (c *Client) handleMessage(ctx context.Context, msg *tg.Message) error {
	// Log all message details for debugging
	c.logger.Debug().
		Str("peer_type", fmt.Sprintf("%T", msg.PeerID)).
		Int("msg_id", msg.ID).
		Str("text_preview", msg.Message[:min(50, len(msg.Message))]).
		Msg("received message")
	
	// Check if message is from our target channel
	peer, ok := msg.PeerID.(*tg.PeerChannel)
	if !ok {
		c.logger.Debug().Msg("not a channel message, skipping")
		return nil
	}
	
	// Telegram stores channel IDs without the -100 prefix
	// Config has: -1003488226342, but message will have: 3488226342
	actualChannelID := c.cfg.ChannelID
	if actualChannelID < 0 {
		// Remove the -100 prefix: -1003488226342 -> 3488226342
		actualChannelID = -(actualChannelID + 1000000000000)
	}
	
	c.logger.Debug().
		Int64("expected_channel_id", actualChannelID).
		Int64("received_channel_id", peer.ChannelID).
		Msg("comparing channel IDs")
	
	if peer.ChannelID != actualChannelID {
		c.logger.Debug().
			Int64("expected", actualChannelID).
			Int64("received", peer.ChannelID).
			Msg("message from different channel, ignoring")
		return nil
	}
	
	c.logger.Info().
		Int64("channel_id", actualChannelID).
		Int("msg_id", msg.ID).
		Msg("✓ new message from target channel")
	
	// Call the handler with the message text
	return c.handler(ctx, msg.Message)
}

type terminalAuth struct {
	phone  string
	logger zerolog.Logger
}

func (a terminalAuth) Phone(_ context.Context) (string, error) {
	a.logger.Info().Str("phone", a.phone).Msg("→ using phone number")
	return a.phone, nil
}

func (a terminalAuth) Password(_ context.Context) (string, error) {
	a.logger.Warn().Msg("══════════════════════════════════════")
	a.logger.Warn().Msg("  2FA PASSWORD REQUIRED")
	a.logger.Warn().Msg("══════════════════════════════════════")
	fmt.Print("Enter 2FA password: ")
	var pwd string
	fmt.Scanln(&pwd)
	return pwd, nil
}

func (a terminalAuth) Code(_ context.Context, sentCode *tg.AuthSentCode) (string, error) {
	a.logger.Info().Msg("══════════════════════════════════════")
	a.logger.Info().Msg("  📱 VERIFICATION CODE SENT")
	a.logger.Info().Msg("══════════════════════════════════════")
	a.logger.Info().Msg("Attempting to read code from Telegram automatically...")
	a.logger.Info().Msg("(Or enter manually if auto-read fails)")
	a.logger.Info().Msg("──────────────────────────────────────")
	
	// Try to extract code from sentCode if available
	if sentCode.Type != nil {
		switch codeType := sentCode.Type.(type) {
		case *tg.AuthSentCodeTypeApp:
			a.logger.Info().Msg("Code sent via Telegram app")
		case *tg.AuthSentCodeTypeSMS:
			a.logger.Info().Msg("Code sent via SMS")
		case *tg.AuthSentCodeTypeCall:
			a.logger.Info().Msg("Code will be called to your phone")
		case *tg.AuthSentCodeTypeFlashCall:
			a.logger.Info().Msg("Flash call verification")
		case *tg.AuthSentCodeTypeMissedCall:
			a.logger.Info().Msg("Missed call verification")
		case *tg.AuthSentCodeTypeFragmentSMS:
			a.logger.Info().Msg("Code sent via Fragment SMS")
		case *tg.AuthSentCodeTypeFirebaseSMS:
			a.logger.Info().Msg("Code sent via Firebase SMS")
		default:
			a.logger.Debug().Str("type", fmt.Sprintf("%T", codeType)).Msg("unknown code type")
		}
	}
	
	// For now, codes are typically sent to Telegram app or SMS
	// We can't auto-read them without being logged in (chicken-egg problem)
	// So we prompt the user
	a.logger.Warn().Msg("⚠️  Auto-read not available during first auth")
	a.logger.Info().Msg("Check your Telegram app or phone for the code")
	
	fmt.Print("\nEnter verification code: ")
	var code string
	fmt.Scanln(&code)
	
	if code == "" {
		return "", fmt.Errorf("no code entered")
	}
	
	a.logger.Info().Str("code", code).Msg("✓ code received, verifying...")
	return code, nil
}

func (a terminalAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	return nil
}

func (a terminalAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("signup not supported")
}


// Connect connects to Telegram without listening (for sending messages only)
func (c *Client) Connect(ctx context.Context) error {
	c.logger.Info().Msg("initializing telegram connection...")

	if err := os.MkdirAll(filepath.Dir(c.cfg.SessionFile), 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	c.client = telegram.NewClient(c.cfg.ApiID, c.cfg.ApiHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: c.cfg.SessionFile,
		},
	})

	return c.client.Run(ctx, func(ctx context.Context) error {
		c.logger.Info().Msg("connecting to telegram servers...")
		c.api = c.client.API()

		c.logger.Debug().Msg("telegram API client initialized")
		c.logger.Info().Msg("checking authentication status...")

		status, err := c.client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("auth status: %w", err)
		}

		if !status.Authorized {
			c.logger.Info().Msg("not authorized, starting phone authentication...")
			// Phone authentication flow would go here
			// For now, return error asking user to run the bot first
			return fmt.Errorf("not authorized - please run the bot first to authenticate")
		}

		c.logger.Info().Msg("already authorized (using saved session)")
		c.logger.Info().Msg("✓ telegram client connected and ready")

		// Keep connection alive
		<-ctx.Done()
		return ctx.Err()
	})
}

// SendMessage sends a text message to a channel
func (c *Client) SendMessage(ctx context.Context, channelID int64, message string) error {
	if c.api == nil {
		return fmt.Errorf("telegram client not connected")
	}

	// Resolve channel
	inputPeer := &tg.InputPeerChannel{
		ChannelID: channelID,
	}

	// Get full channel info to get access hash
	channels, err := c.api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{ChannelID: channelID},
	})
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}

	chats := channels.GetChats()
	if len(chats) == 0 {
		return fmt.Errorf("channel not found")
	}

	channel, ok := chats[0].(*tg.Channel)
	if !ok {
		return fmt.Errorf("not a channel")
	}

	inputPeer.AccessHash = channel.AccessHash

	// Send message
	_, err = c.api.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:    inputPeer,
		Message: message,
	})

	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	c.logger.Info().
		Int64("channel_id", channelID).
		Msg("✓ Message sent to Telegram")

	return nil
}
