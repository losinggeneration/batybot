package main

import (
	"context"
	"strings"

	"github.com/gempir/go-twitch-irc/v4"
)

func setupChatEventHandlers(client *twitch.Client, config *ConfigManager, botUser string) {
	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		log.Debugln("chat: PrivateMessage:", message.Channel, message.User.Name, message.Message)

		// Skip messages from the bot itself
		if strings.EqualFold(message.User.Name, botUser) {
			return
		}
	})

	client.OnNamesMessage(func(message twitch.NamesMessage) {
		log.Debugf("chat: Names: Users in %s: %v", message.Channel, message.Users)
	})

	client.OnUserJoinMessage(func(message twitch.UserJoinMessage) {
		log.Debugf("chat: UserJoin: User joined: %s in %s", message.User, message.Channel)
	})

	client.OnUserPartMessage(func(message twitch.UserPartMessage) {
		log.Debugf("chat: UserPart: User left: %s from %s", message.User, message.Channel)
	})

	client.OnWhisperMessage(func(message twitch.WhisperMessage) {
		log.Debugf("chat: Whisper: Whisper from %s: %s", message.User.DisplayName, message.Message)
	})

	client.OnUnsetMessage(func(message twitch.RawMessage) {
		log.Debugf("chat: Unset: Unhandled message type: %s", message.Raw)
	})

	client.OnUserNoticeMessage(func(message twitch.UserNoticeMessage) {
		log.Debugf("chat: UserNotice: %s in %s - %s", message.MsgID, message.Channel, message.SystemMsg)
	})

	client.OnClearChatMessage(func(message twitch.ClearChatMessage) {
		if message.TargetUserID != "" {
			log.Debugf("chat: ClearChat: User %s was timed out/banned in %s", message.TargetUsername, message.Channel)
		} else {
			log.Debugf("chat: ClearChat: Chat was cleared in %s", message.Channel)
		}
	})

	client.OnClearMessage(func(message twitch.ClearMessage) {
		log.Debugf("chat: Clear: Message deleted in %s: %s", message.Channel, message.Message)
	})

	client.OnSelfPartMessage(func(message twitch.UserPartMessage) {
		log.Debugf("chat: SelfPart: Bot left channel: %s", message.Channel)
	})

	client.OnPingSent(func() {
		log.Trace("chat: Ping sent to Twitch")
	})

	client.OnGlobalUserStateMessage(func(message twitch.GlobalUserStateMessage) {
		log.Debugf("chat: GlobalUserState: %+v", message.User)
	})

	client.OnUserStateMessage(func(message twitch.UserStateMessage) {
		log.Debugf("chat: UserState: change for %s in %s", message.User.DisplayName, message.Channel)
	})

	client.OnNoticeMessage(func(message twitch.NoticeMessage) {
		log.Debugf("chat: Notice: in %s [%s]: %s", message.Channel, message.MsgID, message.Message)

		switch message.MsgID {
		case "msg_banned":
			log.Warn("chat: Bot is banned from this channel")
		case "msg_channel_suspended":
			log.Warn("chat: Channel is suspended")
		case "msg_ratelimit":
			log.Warn("chat: Rate limit exceeded")
		}
	})

	client.OnPingMessage(func(message twitch.PingMessage) {
		log.Trace("chat: Received PING, responding with PONG")
	})

	client.OnPongMessage(func(message twitch.PongMessage) {
		log.Trace("chat: Received PONG")
	})

	client.OnRoomStateMessage(func(message twitch.RoomStateMessage) {
		log.Debugf("chat: RoomState: change in %s: %+v", message.Channel, message.State)
	})

	client.OnConnect(func() {
		log.Debug("chat: Connected to Twitch!")
	})

	client.OnReconnectMessage(func(message twitch.ReconnectMessage) {
		log.Debug("chat: Reconnect: Received reconnect message from Twitch")
		tokenRefresh(context.Background(), client, config, BotTokenType)
	})

	client.OnSelfJoinMessage(func(message twitch.UserJoinMessage) {
		log.Debugf("chat: SelfJoin: Bot joined channel: %s", message.Channel)

		if users, err := client.Userlist(message.Channel); err == nil {
			log.Debugf("chat: SelfJoin: Channel %s has %d users", message.Channel, len(users))
		}
	})
}
