package main

import (
	"strings"

	"github.com/gempir/go-twitch-irc/v4"
)

func setupEventHandlers(client *twitch.Client, botUser string) {
	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		log.Debugln(message.Channel, message.User.Name, message.Message)

		// Skip messages from the bot itself
		if strings.EqualFold(message.User.Name, botUser) {
			return
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
