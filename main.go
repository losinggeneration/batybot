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

func init() {
	log = logrus.New()
	if level := strings.TrimSpace(os.Getenv("LOG_LEVEL")); level != "" {
		log.Infof("Trying to set log level to %q", level)
		l, err := logrus.ParseLevel(level)
		if err != nil {
			log.Infof("Invalid log level %q", level)
			return
		}

		log.SetLevel(l)
	}
}

func main() {
	requiredEnvVars := []string{"TWITCH_CLIENT_ID", "TWITCH_CLIENT_SECRET", "TWITCH_USER", "TWITCH_CHANNEL"}
	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			log.Fatalf("Required environment variable %s is not set", envVar)
		}
	}

	token := os.Getenv("TWITCH_TOKEN")
	refresh := os.Getenv("TWITCH_REFRESH")
	expires := os.Getenv("TWITCH_EXPIRES")

	if token == "" || refresh == "" || expires == "" {
		log.Info("No valid tokens found, starting OAuth flow...")
		log.Info("Please open your browser and navigate to http://localhost:8080 to authorize the bot")

		creds, err := getToken()
		if err != nil {
			log.Errorf("unable to get access token: %v", err)
			panic(err)
		}

		log.Info("Authorization successful! Bot is starting...")
		token, refresh, expires = creds.get()
	} else {
		log.Info("Using existing tokens")
	}

	user := os.Getenv("TWITCH_USER")
	if user == "" {
		log.Fatalf("expected a user, set TWITCH_USER environment variable")
	}

	token = strings.TrimPrefix(token, "oauth:")
	client := twitch.NewClient("batybot", token)

	if os.Getenv("BOT_VERIFIED") == "true" {
		client.SetJoinRateLimiter(twitch.CreateVerifiedRateLimiter())
		log.Info("Using verified bot rate limiter")
	} else {
		client.SetJoinRateLimiter(twitch.CreateDefaultRateLimiter())
		log.Info("Using default rate limiter")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	setupEventHandlers(client, user)

	wg.Add(1)
	go func() {
		defer wg.Done()
		doRefresh(ctx, client, refresh, expires)
	}()

	channel := os.Getenv("TWITCH_CHANNEL")
	if channel == "" {
		log.Fatal("expected TWITCH_CHANNEL to be set")
		panic("TWITCH_CHANNEL unset")
	}

	client.Join(channel)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := client.Connect(); err != nil {
			log.Errorf("unable to connect: %v", err)
			cancel()
		}
	}()

	log.Infof("Batybot started! Connected as %s in #%s", user, channel)

	<-sigChan
	log.Info("Shutdown signal received, shutting down...")

	cancel()

	if client != nil {
		log.Info("Disconnecting from Twitch...")
		if err := client.Disconnect(); err != nil {
			log.Info("Unable to disconnect, exiting anyway")
			return
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

		if strings.Contains(strings.ToLower(message.Message), "batybot") && time.Since(lastMention) > 5*time.Minute {
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

func doRefresh(ctx context.Context, client *twitch.Client, refresh, expires string) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Token refresh routine stopping")
			return
		default:
		}

		expiresAt, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			log.Errorf("unable to parse expires time: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Minute):
				continue
			}
		}

		if time.Now().After(expiresAt) {
			log.Warnf("refresh token is already expired at %s", expiresAt)
		}

		// Refresh when 10 minutes are left (or immediately if already expired)
		refreshTime := expiresAt.Add(-10 * time.Minute)
		if refreshTime.Before(time.Now()) {
			refreshTime = time.Now()
		}

		until := time.Until(refreshTime)
		log.Debugf("Waiting %v before refreshing token that expires at %s", until, expires)

		select {
		case <-ctx.Done():
			log.Info("Token refresh routine stopping during wait")
			return
		case <-time.After(until):
		}

		log.Info("Refreshing token...")
		creds, err := refreshToken(refresh)
		if err != nil {
			log.Errorf("Failed to refresh token: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				continue
			}
		}

		var token string
		token, refresh, expires = creds.get()

		token = strings.TrimPrefix(token, "oauth:")

		log.Info("Token refreshed successfully")

		client.SetIRCToken(token)

		log.Debugf("New token expires at: %s", expires)
	}
}
