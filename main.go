package main

import (
	"os"
	"strings"
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
	token := os.Getenv("TWITCH_TOKEN")
	refresh := os.Getenv("TWITCH_REFRESH")
	expires := os.Getenv("TWITCH_EXPIRES")

	if token == "" || refresh == "" || expires == "" {
		creds, err := getToken()
		if err != nil {
			log.Debugln("unable to get access token")
			panic(err)
		}

		log.Debugf("%#v", creds)

		token, refresh, expires = creds.get()
	}

	user := os.Getenv("TWITCH_USER")
	if user == "" {
		log.Fatalf("expected a user, set TWITCH_USER environment variable")
	}

	token = strings.TrimPrefix(token, "oauth:")
	client := twitch.NewClient("batybot", token)

	client.OnNoticeMessage(func(message twitch.NoticeMessage) {
		log.Debugf("notice message: %#v", message)
	})

	go doRefresh(client, refresh, expires)

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

		if strings.Contains(strings.ToLower(message.Message), "batybot") && time.Since(lastMention) > 5*time.Minute {
			lastMention = time.Now()
			client.Say(message.Channel, "What? No, I'm awake BatPls")
		}
	})

	client.OnNamesMessage(func(message twitch.NamesMessage) {
		log.Debugf("names message: %#v", message)
	})

	client.OnRoomStateMessage(func(message twitch.RoomStateMessage) {
		log.Debugf("room state message: %#v", message)
	})

	client.OnConnect(func() {
		log.Info("connected")
	})

	client.OnReconnectMessage(func(message twitch.ReconnectMessage) {
		log.Info("received reconnect message")
	})

	channel := os.Getenv("TWITCH_CHANNEL")
	if channel == "" {
		log.Fatal("expected TWITCH_CHANNEL to be set")
		panic("TWITCH_CHANNEL unset")
	}

	client.Join(channel)

	if err := client.Connect(); err != nil {
		log.Errorf("unable to connect %#v", token)
		panic(err)
	}
}

func doRefresh(client *twitch.Client, refresh, expires string) {
	for {
		expiresAt, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			log.Errorf("unable to parse expires time: %v", err)
			// Wait a bit and continue to prevent tight loop
			time.Sleep(1 * time.Minute)
			continue
		}

		// Check if token is already expired
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
		time.Sleep(until)

		log.Info("Refreshing token...")
		creds, err := refreshToken(refresh)
		if err != nil {
			log.Errorf("Failed to refresh token: %v", err)
			// Wait a bit before retrying
			time.Sleep(30 * time.Second)
			continue
		}

		var token string
		token, refresh, expires = creds.get()

		token = strings.TrimPrefix(token, "oauth:")

		log.Info("Token refreshed successfully")

		client.SetIRCToken(token)

		log.Debugf("New token expires at: %s", expires)
	}
}
