package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gempir/go-twitch-irc/v4"
)

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
		log.Debugf("Whisper from %s: %s", message.User.DisplayName, message.Message)
	})

	client.OnUnsetMessage(func(message twitch.RawMessage) {
		log.Debugf("Unhandled message type: %s", message.Raw)
	})

	client.OnUserNoticeMessage(func(message twitch.UserNoticeMessage) {
		log.Debugf("User notice: %s in %s - %s", message.MsgID, message.Channel, message.SystemMsg)

		switch message.MsgID {
		case "sub", "resub":
			log.Debugf("New subscriber: %s", message.User.DisplayName)
			client.Say(message.Channel, fmt.Sprintf("Welcome %s! Thanks for the sub! BatJAM", message.User.DisplayName))
		case "subgift":
			log.Debugf("Gift sub from %s", message.User.DisplayName)
			client.Say(message.Channel, fmt.Sprintf("Thanks for the gift sub %s! BatPop", message.User.DisplayName))
		case "raid":
			if raiderCount, ok := message.MsgParams["msg-param-viewerCount"]; ok {
				log.Debugf("Raid from %s with %s viewers", message.User.DisplayName, raiderCount)
				client.Say(message.Channel, fmt.Sprintf("Welcome raiders from %s! BatJAM BatJAM BatJAM", message.User.DisplayName))
			}
		case "ritual":
			if ritual, ok := message.MsgParams["msg-param-ritual-name"]; ok && ritual == "new_chatter" {
				log.Debugf("New chatter: %s", message.User.DisplayName)
				client.Say(message.Channel, fmt.Sprintf("Welcome to chat %s! BatPls", message.User.DisplayName))
			}
		}
	})

	client.OnClearChatMessage(func(message twitch.ClearChatMessage) {
		if message.TargetUserID != "" {
			log.Debugf("User %s was timed out/banned in %s", message.TargetUsername, message.Channel)
		} else {
			log.Debugf("Chat was cleared in %s", message.Channel)
		}
	})

	client.OnClearMessage(func(message twitch.ClearMessage) {
		log.Debugf("Message deleted in %s: %s", message.Channel, message.Message)
	})

	client.OnSelfPartMessage(func(message twitch.UserPartMessage) {
		log.Debugf("Bot left channel: %s", message.Channel)
	})

	client.OnPingSent(func() {
		log.Trace("Ping sent to Twitch")
	})

	client.OnGlobalUserStateMessage(func(message twitch.GlobalUserStateMessage) {
		log.Debugf("Global user state: %+v", message.User)
	})

	client.OnUserStateMessage(func(message twitch.UserStateMessage) {
		log.Debugf("User state change for %s in %s", message.User.DisplayName, message.Channel)
	})

	client.OnNoticeMessage(func(message twitch.NoticeMessage) {
		log.Debugf("Notice in %s [%s]: %s", message.Channel, message.MsgID, message.Message)

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
		log.Trace("Received PING, responding with PONG")
	})

	client.OnPongMessage(func(message twitch.PongMessage) {
		log.Trace("Received PONG")
	})

	client.OnRoomStateMessage(func(message twitch.RoomStateMessage) {
		log.Debugf("Room state change in %s: %+v", message.Channel, message.State)
	})

	client.OnConnect(func() {
		log.Debug("Connected to Twitch!")
	})

	client.OnReconnectMessage(func(message twitch.ReconnectMessage) {
		log.Debug("Received reconnect message from Twitch")
	})

	client.OnSelfJoinMessage(func(message twitch.UserJoinMessage) {
		log.Debugf("Bot joined channel: %s", message.Channel)

		if users, err := client.Userlist(message.Channel); err == nil {
			log.Debugf("Channel %s has %d users", message.Channel, len(users))
		}
	})
}
