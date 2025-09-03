package main

import (
	"context"
	"fmt"
	"sync"

	irc "github.com/gempir/go-twitch-irc/v4"
	eventsub "github.com/joeyak/go-twitch-eventsub/v3"
	helix "github.com/nicklaw5/helix/v2"
)

type EventSubManager struct {
	client     *eventsub.Client
	chatClient *irc.Client
	config     *ConfigManager
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func NewEventSubManager(chatClient *irc.Client, config *ConfigManager) *EventSubManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &EventSubManager{
		chatClient: chatClient,
		config:     config,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (esm *EventSubManager) Start() error {
	log.Debug("Starting EventSub manager...")

	esm.client = eventsub.NewClient()

	broadcasterID, err := esm.getBroadcasterID()
	if err != nil {
		return fmt.Errorf("failed to get broadcaster ID: %w", err)
	}

	esm.setupEventHandlers()

	esm.client.OnWelcome(func(message eventsub.WelcomeMessage) {
		log.Debug("EventSub WebSocket connected")

		if err := esm.subscribeToEvents(broadcasterID, message.Payload.Session.ID); err != nil {
			log.Errorf("Failed to subscribe to events: %v", err)
		}
	})

	esm.client.OnError(func(err error) {
		log.Errorf("EventSub error: %v", err)
	})

	esm.client.OnKeepAlive(func(message eventsub.KeepAliveMessage) {
		log.Trace("EventSub keepalive received")
	})

	esm.client.OnReconnect(func(message eventsub.ReconnectMessage) {
		log.Debug("EventSub reconnect requested")
	})

	esm.wg.Add(1)
	go func() {
		defer esm.wg.Done()
		if err := esm.client.ConnectWithContext(esm.ctx); err != nil {
			log.Errorf("EventSub client error: %v", err)
		}
	}()

	log.Debug("EventSub manager started successfully")
	return nil
}

func (esm *EventSubManager) Stop() {
	log.Debug("Stopping EventSub manager...")

	esm.cancel()

	if esm.client != nil {
		if err := esm.client.Close(); err != nil {
			log.Errorf("unable to close EventSub client: %v", err)
		}
	}

	esm.wg.Wait()
	log.Debug("EventSub manager stopped")
}

// getBroadcasterID retrieves the broadcaster's user ID from their username
func (esm *EventSubManager) getBroadcasterID() (string, error) {
	token := esm.config.GetBroadcasterTokens()
	twitchConfig := esm.config.Twitch()

	client, err := helix.NewClient(&helix.Options{
		ClientID:     twitchConfig.ClientID,
		ClientSecret: twitchConfig.ClientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create Helix client: %w", err)
	}

	client.SetUserAccessToken(token.AccessToken)

	resp, err := client.GetUsers(&helix.UsersParams{
		Logins: []string{twitchConfig.Broadcaster}, // Use broadcaster username
	})
	if err != nil {
		return "", fmt.Errorf("failed to get user info: %w", err)
	}

	if len(resp.Data.Users) == 0 {
		return "", fmt.Errorf("user %s not found", twitchConfig.Broadcaster)
	}

	broadcasterID := resp.Data.Users[0].ID
	log.Debugf("Found broadcaster ID: %s for broadcaster: %s", broadcasterID, twitchConfig.Broadcaster)

	return broadcasterID, nil
}

// setupEventHandlers configures all the event handlers we care about
func (esm *EventSubManager) setupEventHandlers() {
	log.Debug("Setting up EventSub event handlers...")

	esm.client.OnEventChannelSubscribe(esm.handleChannelSubscribe)
	esm.client.OnEventChannelSubscriptionGift(esm.handleChannelSubscriptionGift)
	esm.client.OnEventChannelSubscriptionMessage(esm.handleChannelSubscriptionMessage)

	esm.client.OnEventChannelFollow(esm.handleChannelFollow)

	esm.client.OnEventChannelRaid(esm.handleChannelRaid)

	esm.client.OnEventChannelCheer(esm.handleChannelCheer)

	esm.client.OnEventChannelUpdate(esm.handleChannelUpdate)

	esm.client.OnEventStreamOnline(esm.handleStreamOnline)
	esm.client.OnEventStreamOffline(esm.handleStreamOffline)

	// Chat notification events (in case there are misses above)
	esm.client.OnEventChannelChatNotification(esm.handleChannelChatNotification)

	log.Debug("EventSub event handlers configured")
}

// subscribeToEvents subscribes to all desired EventSub events
func (esm *EventSubManager) subscribeToEvents(broadcasterID, sessionID string) error {
	log.Debug("Subscribing to EventSub events...")

	token := esm.config.GetBroadcasterTokens()
	twitchConfig := esm.config.Twitch()

	broadcasterCondition := map[string]string{"broadcaster_user_id": broadcasterID}

	subscriptions := []eventsub.SubscribeRequest{{
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken, // Use broadcaster token
		Event:       eventsub.SubChannelSubscribe,
		Condition:   broadcasterCondition,
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken, // Use broadcaster token
		Event:       eventsub.SubChannelSubscriptionGift,
		Condition:   broadcasterCondition,
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken, // Use broadcaster token
		Event:       eventsub.SubChannelSubscriptionMessage,
		Condition:   broadcasterCondition,
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken, // Use broadcaster token
		Event:       eventsub.SubChannelFollow,
		Condition: map[string]string{
			"broadcaster_user_id": broadcasterID,
			"moderator_user_id":   broadcasterID,
		},
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken, // Use broadcaster token
		Event:       eventsub.SubChannelRaid,
		Condition:   map[string]string{"to_broadcaster_user_id": broadcasterID}, // Fixed condition
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken, // Use broadcaster token
		Event:       eventsub.SubChannelCheer,
		Condition:   broadcasterCondition,
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken,
		Event:       eventsub.SubChannelUpdate,
		Condition:   broadcasterCondition,
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken,
		Event:       eventsub.SubStreamOnline,
		Condition:   broadcasterCondition,
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken,
		Event:       eventsub.SubStreamOffline,
		Condition:   broadcasterCondition,
	}, {
		SessionID:   sessionID,
		ClientID:    twitchConfig.ClientID,
		AccessToken: token.AccessToken, // Use broadcaster token
		Event:       eventsub.SubChannelChatNotification,
		Condition: map[string]string{
			"broadcaster_user_id": broadcasterID,
			"user_id":             broadcasterID, // Use broadcaster's user ID, not bot's
		},
	}}

	for _, sub := range subscriptions {
		resp, err := eventsub.SubscribeEvent(sub)
		if err != nil {
			log.Warnf("Failed to subscribe to %s: %v", sub.Event, err)
			continue
		}

		if len(resp.Data) > 0 {
			log.Debugf("Subscribed to %s (ID: %s, Cost: %d)",
				sub.Event, resp.Data[0].ID, resp.Data[0].Cost)
		}
	}

	log.Debug("EventSub subscription setup complete")
	return nil
}

func (esm *EventSubManager) handleChannelSubscribe(event eventsub.EventChannelSubscribe) {
	log.Debugf("New subscriber: %s (Tier: %s)", event.UserName, event.Tier)
}

func (esm *EventSubManager) handleChannelSubscriptionGift(event eventsub.EventChannelSubscriptionGift) {
	if event.IsAnonymous {
		log.Debugf("Anonymous gift sub: %d subs gifted (Tier: %s)", event.Total, event.Tier)
	} else {
		log.Debugf("Gift sub from %s: %d subs gifted (Tier: %s)", event.UserName, event.Total, event.Tier)
	}
}

func (esm *EventSubManager) handleChannelSubscriptionMessage(event eventsub.EventChannelSubscriptionMessage) {
	log.Debugf("Sub message from %s (Tier: %s, Months: %d): %s",
		event.UserName, event.Tier, event.CumulativeMonths, event.Message.Text)
}

func (esm *EventSubManager) handleChannelFollow(event eventsub.EventChannelFollow) {
	log.Debugf("New follower: %s (followed at: %s)", event.UserName, event.FollowedAt)
}

func (esm *EventSubManager) handleChannelRaid(event eventsub.EventChannelRaid) {
	log.Debugf("Raid from %s with %d viewers", event.FromBroadcasterUserName, event.Viewers)
}

func (esm *EventSubManager) handleChannelCheer(event eventsub.EventChannelCheer) {
	if event.IsAnonymous {
		log.Debugf("Anonymous cheer: %d bits", event.Bits)
	} else {
		log.Debugf("Cheer from %s: %d bits - %s", event.UserName, event.Bits, event.Message)
	}
}

func (esm *EventSubManager) handleChannelUpdate(event eventsub.EventChannelUpdate) {
	log.Debugf("Channel updated - Title: %s, Category: %s", event.Title, event.CategoryName)
}

func (esm *EventSubManager) handleStreamOnline(event eventsub.EventStreamOnline) {
	log.Debugf("Stream went online - Type: %s, Started at: %s", event.Type, event.StartedAt)
}

func (esm *EventSubManager) handleStreamOffline(event eventsub.EventStreamOffline) {
	log.Debugf("Stream went offline")
}

func (esm *EventSubManager) handleChannelChatNotification(event eventsub.EventChannelChatNotification) {
	prefix := "handleChannelChatNotification"
	log.Debugf("%s Chat notification - Type: %s, System: %s", prefix, event.NoticeType, event.SystemMessage)

	twitchConfig := esm.config.Twitch()

	switch event.NoticeType {
	case "sub":
		if event.Sub != nil {
			message := fmt.Sprintf("%s: Welcome %s! Thanks for the sub! BatJAM", prefix, event.ChatterUserName)
			log.Debugf(twitchConfig.Channel, message)
		}
	case "resub":
		if event.Resub != nil {
			message := fmt.Sprintf("%s Thanks for the resub %s! %d months strong! BatJAM",
				prefix, event.ChatterUserName, event.Resub.CumulativeMonths)
			log.Debugf(twitchConfig.Channel, message)
		}
	case "sub_gift":
		if event.SubGift != nil {
			message := fmt.Sprintf("%s Thanks %s for the gift sub! BatPop", prefix, event.ChatterUserName)
			log.Debugf(twitchConfig.Channel, message)
		}
	case "community_sub_gift":
		if event.CommunitySubGift != nil {
			message := fmt.Sprintf("%s Thanks %s for gifting %d subs! BatPop",
				prefix, event.ChatterUserName, event.CommunitySubGift.Total)
			log.Debugf(twitchConfig.Channel, message)
		}
	case "raid":
		if event.Raid != nil {
			message := fmt.Sprintf("%s Welcome raiders from %s! BatJAM BatJAM BatJAM",
				prefix, event.Raid.UserName)
			log.Debugf(twitchConfig.Channel, message)
		}
	case "announcement":
		log.Debugf("Announcement from %s: %s", event.ChatterUserName, event.Message.Text)
	}
}

// RefreshToken updates the EventSub client with a new access token
// TODO The v3 library doesn't seem to have a direct token update method,
// so we might need to reconnect or handle this differently
func (esm *EventSubManager) RefreshToken(newToken string) {
	log.Debug("Token refreshed - EventSub may need to reconnect")
}
