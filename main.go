package main

import (
	"fmt"
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

// This isn't working to keep the token valid
func doRefresh(client *twitch.Client, refresh, expires string) {
	for {
		expiresAt, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			panic(fmt.Errorf("unable to parse expires time: %w", err))
		} else if time.Now().After(expiresAt) {
			panic(fmt.Errorf("refresh token %s is already expired", expiresAt))
		}

		const early = 400
		until := time.Until(expiresAt) / early
		log.Debugf("Waiting %v before refreshing token that expires %s", until, expires)
		time.Sleep(until)

		creds, err := refreshToken(refresh)
		if err != nil {
			panic(err)
		}

		var token string
		token, refresh, expires = creds.get()
		client.SetIRCToken(token)

		err = client.Connect()
		if err != nil {
			log.Errorf("unable to connect %#v", token)
			panic(err)
		}
	}
}
