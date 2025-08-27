package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

func setup() (*twitch.Client, *ConfigManager) {
	log = logrus.New()
	config, err := InitConfig()
	if err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}

	if level := config.Logging().Level; level != "" {
		log.Infof("Setting log level to %q", level)
		l, err := logrus.ParseLevel(level)
		if err != nil {
			log.Warnf("Invalid log level %q, using info", level)
			log.SetLevel(logrus.InfoLevel)
		} else {
			log.SetLevel(l)
		}
	}

	log.Info("Starting Batybot...")

	if !config.HasValidTokens() {
		log.Info("Starting OAuth flow...")
		log.Infof("Open your browser to http://localhost:%s to authorize the bot", config.Server().OAuthPort)

		if err := performOAuthFlow(config); err != nil {
			log.Fatalf("OAuth failed: %v", err)
		}

		log.Info("Authorization successful!")
	}

	accessToken, _, expiresAt := config.GetTokens()
	log.Debugf("Token expires at: %v", expiresAt)

	accessToken = strings.TrimPrefix(accessToken, "oauth:")
	client := twitch.NewClient("batybot", accessToken)

	if config.Bot().Verified {
		client.SetJoinRateLimiter(twitch.CreateVerifiedRateLimiter())
		log.Info("Using verified bot rate limiter")
	} else {
		client.SetJoinRateLimiter(twitch.CreateDefaultRateLimiter())
		log.Info("Using default rate limiter")
	}

	return client, config
}

func main() {
	client, config := setup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	twitchConfig := config.Twitch()
	setupEventHandlers(client, twitchConfig.User)

	wg.Add(1)
	go func() {
		defer wg.Done()
		tokenRefreshWatch(ctx, client, config)
	}()

	client.Join(twitchConfig.Channel)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := client.Connect(); err != nil {
			log.Errorf("Unable to connect: %v", err)
			cancel()
		}
	}()

	log.Infof("Batybot started! Connected as %s in #%s", twitchConfig.User, twitchConfig.Channel)
	log.Info("Press Ctrl+C to gracefully shutdown")

	<-sigChan
	log.Info("Shutdown signal received, shutting down...")

	cancel()

	shutdown(client, &wg)
}

func shutdown(client *twitch.Client, wg *sync.WaitGroup) {
	if client != nil {
		log.Info("Disconnecting from Twitch...")
		if err := client.Disconnect(); err != nil {
			log.Warn("Unable to disconnect cleanly, forcing exit")
		}
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info("Batybot shutdown complete")
	case <-time.After(10 * time.Second):
		log.Warn("Shutdown timeout exceeded, forcing exit")
	}
}

func setupEventHandlers(client *twitch.Client, botUser string) {
	lastMention := time.Now()

	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		log.Debugln(message.Channel, message.User.Name, message.Message)

		// Skip messages from the bot itself
		if strings.EqualFold(message.User.Name, botUser) {
			return
		}

		msg := strings.ToLower(message.Message)
		switch {
		case strings.Contains(msg, "batjam"):
			client.Say(message.Channel, "BatJAM BatJAM BatJAM")
		case strings.Contains(msg, "batpop"):
			client.Say(message.Channel, "BatPop BatPop BatPop")
		case strings.HasSuffix(msg, "batg"):
			client.Say(message.Channel, "very interesting BatG")
		}

		if message.User.Badges["subscriber"] != 0 {
			log.Debugf("Message from subscriber: %s", message.User.DisplayName)
		}
		if message.User.Badges["moderator"] != 0 {
			log.Debugf("Message from moderator: %s", message.User.DisplayName)
		}
		if message.User.Badges["broadcaster"] != 0 {
			log.Debugf("Message from broadcaster: %s", message.User.DisplayName)
		}

		if strings.Contains(msg, "batybot") && time.Since(lastMention) > 5*time.Minute {
			lastMention = time.Now()
			client.Say(message.Channel, "What? No, I'm awake BatPls")
		}
	})

	client.OnNamesMessage(func(message twitch.NamesMessage) {
		log.Debugf("Users in %s: %v", message.Channel, message.Users)
	})

	client.OnUserJoinMessage(func(message twitch.UserJoinMessage) {
		log.Debugf("User joined: %s in %s", message.User, message.Channel)
	})

	client.OnUserPartMessage(func(message twitch.UserPartMessage) {
		log.Debugf("User left: %s from %s", message.User, message.Channel)
	})

	client.OnWhisperMessage(func(message twitch.WhisperMessage) {
		log.Infof("Whisper from %s: %s", message.User.DisplayName, message.Message)
	})

	client.OnUnsetMessage(func(message twitch.RawMessage) {
		log.Debugf("Unhandled message type: %s", message.Raw)
	})

	client.OnUserNoticeMessage(func(message twitch.UserNoticeMessage) {
		log.Debugf("User notice: %s in %s - %s", message.MsgID, message.Channel, message.SystemMsg)

		switch message.MsgID {
		case "sub", "resub":
			log.Infof("New subscriber: %s", message.User.DisplayName)
			client.Say(message.Channel, fmt.Sprintf("Welcome %s! Thanks for the sub! BatJAM", message.User.DisplayName))
		case "subgift":
			log.Infof("Gift sub from %s", message.User.DisplayName)
			client.Say(message.Channel, fmt.Sprintf("Thanks for the gift sub %s! BatPop", message.User.DisplayName))
		case "raid":
			if raiderCount, ok := message.MsgParams["msg-param-viewerCount"]; ok {
				log.Infof("Raid from %s with %s viewers", message.User.DisplayName, raiderCount)
				client.Say(message.Channel, fmt.Sprintf("Welcome raiders from %s! BatJAM BatJAM BatJAM", message.User.DisplayName))
			}
		case "ritual":
			if ritual, ok := message.MsgParams["msg-param-ritual-name"]; ok && ritual == "new_chatter" {
				log.Infof("New chatter: %s", message.User.DisplayName)
				client.Say(message.Channel, fmt.Sprintf("Welcome to chat %s! BatPls", message.User.DisplayName))
			}
		}
	})

	client.OnClearChatMessage(func(message twitch.ClearChatMessage) {
		if message.TargetUserID != "" {
			log.Infof("User %s was timed out/banned in %s", message.TargetUsername, message.Channel)
		} else {
			log.Infof("Chat was cleared in %s", message.Channel)
		}
	})

	client.OnClearMessage(func(message twitch.ClearMessage) {
		log.Infof("Message deleted in %s: %s", message.Channel, message.Message)
	})

	client.OnSelfPartMessage(func(message twitch.UserPartMessage) {
		log.Infof("Bot left channel: %s", message.Channel)
	})

	client.OnPingSent(func() {
		log.Debug("Ping sent to Twitch")
	})

	client.OnGlobalUserStateMessage(func(message twitch.GlobalUserStateMessage) {
		log.Debugf("Global user state: %+v", message.User)
	})

	client.OnUserStateMessage(func(message twitch.UserStateMessage) {
		log.Debugf("User state change for %s in %s", message.User.DisplayName, message.Channel)
	})

	client.OnNoticeMessage(func(message twitch.NoticeMessage) {
		log.Infof("Notice in %s [%s]: %s", message.Channel, message.MsgID, message.Message)

		switch message.MsgID {
		case "msg_banned":
			log.Warn("Bot is banned from this channel")
		case "msg_channel_suspended":
			log.Warn("Channel is suspended")
		case "msg_ratelimit":
			log.Warn("Rate limit exceeded")
		}
	})

	client.OnPingMessage(func(message twitch.PingMessage) {
		log.Debug("Received PING, responding with PONG")
	})

	client.OnPongMessage(func(message twitch.PongMessage) {
		log.Debug("Received PONG")
	})

	client.OnRoomStateMessage(func(message twitch.RoomStateMessage) {
		log.Debugf("Room state change in %s: %+v", message.Channel, message.State)
	})

	client.OnConnect(func() {
		log.Info("Connected to Twitch!")
	})

	client.OnReconnectMessage(func(message twitch.ReconnectMessage) {
		log.Info("Received reconnect message from Twitch")
	})

	client.OnSelfJoinMessage(func(message twitch.UserJoinMessage) {
		log.Infof("Bot joined channel: %s", message.Channel)

		if users, err := client.Userlist(message.Channel); err == nil {
			log.Infof("Channel %s has %d users", message.Channel, len(users))
		}
	})
}

func tokenDelay(ctx context.Context, config *ConfigManager) bool {
	_, _, expiresAt := config.GetTokens()
	if time.Now().After(expiresAt) {
		log.Warnf("refresh token is already expired at %s", expiresAt)
	}

	// Refresh when 10 minutes are left (or immediately if already expired)
	refreshTime := expiresAt.Add(-10 * time.Minute)
	if refreshTime.Before(time.Now()) {
		refreshTime = time.Now()
	}

	until := time.Until(refreshTime)
	log.Debugf("Waiting %v before refreshing token that expires at %s", until, expiresAt)

	select {
	case <-ctx.Done():
		log.Info("Token refresh routine stopping during wait")
		return true
	case <-time.After(until):
	}

	return false
}

func tokenRefresh(ctx context.Context, client *twitch.Client, config *ConfigManager) bool {
	log.Info("Refreshing token...")

	_, refreshToken, _ := config.GetTokens()
	newTokens, err := refreshTokens(config, refreshToken)
	if err != nil {
		log.Errorf("Failed to refresh token: %v", err)
		select {
		case <-ctx.Done():
			return true
		case <-time.After(30 * time.Second):
			return false
		}
	}

	accessToken, refreshToken, expiresAt := newTokens.get()
	config.SetTokens(accessToken, refreshToken, parseExpiresTime(expiresAt))

	token := strings.TrimPrefix(accessToken, "oauth:")
	client.SetIRCToken(token)

	log.Info("Token refreshed successfully")
	log.Debugf("New token expires at: %s", expiresAt)

	return false
}

func tokenRefreshWatch(ctx context.Context, client *twitch.Client, config *ConfigManager) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Token refresh routine stopping")
			return
		default:
		}

		if tokenDelay(ctx, config) {
			return
		}

		if tokenRefresh(ctx, client, config) {
			return
		}
	}
}
