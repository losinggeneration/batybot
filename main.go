package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/gempir/go-twitch-irc/v3"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	otwitch "golang.org/x/oauth2/twitch"
)

var log *logrus.Logger

func init() {
	log = logrus.New()
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		l, err := logrus.ParseLevel(level)
		if err != nil {
			log.SetLevel(l)
		}
	}
}

func getToken() (*oauth2.Token, error) {
	oauth2Config := &clientcredentials.Config{
		ClientID:     os.Getenv("TWITCH_CLIENT_ID"),
		ClientSecret: os.Getenv("TWITCH_CLIENT_SECRET"),
		TokenURL:     otwitch.Endpoint.TokenURL,
	}

	log.Debugln(oauth2Config)
	return oauth2Config.Token(context.Background())
}

func main() {
	token := os.Getenv("TWITCH_TOKEN")
	if token == "" {
		t, err := getToken()
		if err != nil {
			log.Debugln("unable to get access token")
			panic(err)
		}

		token = t.AccessToken
	}
	user := os.Getenv("TWITCH_USER")
	if user == "" {
		log.Fatalf("expected a user, set TWITCH_USER environment variable")
	}

	client := twitch.NewClient("batybot", token)

	client.OnNoticeMessage(func(message twitch.NoticeMessage) {
		log.Debugf("notice message: %#v", message)
	})

	lastMention := time.Now()
	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		log.Debugln(message.Channel, message.Message)
		if strings.Contains(strings.ToLower(message.Message), "batjam") {
			log.Debugln(message.Channel, message.User.Name, message.Message)
			client.Say(message.Channel, "BatJAM BatJAM BatJAM")
		}

		if strings.Contains(strings.ToLower(message.Message), "batpop") {
			log.Debugln(message.Channel, message.User.Name, message.Message)
			client.Say(message.Channel, "BatPop BatPop BatPop")
		}

		if strings.HasSuffix(strings.ToLower(message.Message), "batg") {
			log.Debugln(message.Channel, message.User.Name, message.Message)
			client.Say(message.Channel, "very interesting BatG")
		}

		if strings.Contains(strings.ToLower(message.Message), "batybot") && time.Now().Sub(lastMention) > 5*time.Minute {
			lastMention = time.Now()
			log.Debugln(message.Channel, message.User.Name, message.Message)
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
		log.Debug("connected")
	})

	channel := os.Getenv("TWITCH_CHANNEL")
	if channel == "" {
		log.Fatal("expected TWITCH_CHANNEL to be set")
		panic("TWITCH_CHANNEL unset")
	}

	client.Join(channel)
	err := client.Connect()
	if err != nil {
		log.Errorf("unable to connect %#v", token)
		panic(err)
	}
}
