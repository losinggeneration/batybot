package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	irc "github.com/gempir/go-twitch-irc/v4"
	"github.com/sirupsen/logrus"
)

type refreshControl int

const (
	refreshControlStop     = 1
	refreshControlContinue = 2
)

var log *logrus.Logger

func prefixToken(token string) string {
	if strings.HasPrefix(token, "oauth:") {
		return token
	}

	return "oauth:" + token
}

func setup() (*irc.Client, *ConfigManager) {
	log = logrus.New()

	var cfg string
	flag.StringVar(&cfg, "config", "", "config file to use")
	flag.Parse()

	config, err := InitConfig(cfg)
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

	if err := oauthFlow(config); err != nil {
		log.Fatalf("Auth failed: %v", err)
	}

	token := config.GetBotTokens()
	log.Debugf("Bot token expires at: %v", token.ExpiresAt)

	client := irc.NewClient("batybot", prefixToken(token.AccessToken))

	if config.Bot().Verified {
		client.SetJoinRateLimiter(irc.CreateVerifiedRateLimiter())
		log.Info("Using verified bot rate limiter")
	} else {
		client.SetJoinRateLimiter(irc.CreateDefaultRateLimiter())
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

	esm := NewEventSubManager(client, config)
	if err := esm.Start(); err != nil {
		log.Warnf("Failed to start EventSub manager: %v", err)
		log.Info("Continuing without EventSub support...")
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		tokenRefreshWatch(ctx, client, config, BotTokenType)
	}()

	go func() {
		defer wg.Done()
		tokenRefreshWatch(ctx, client, config, BroadcasterTokenType)
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

func shutdown(client *irc.Client, wg *sync.WaitGroup) {
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

func tokenDelay(ctx context.Context, config *ConfigManager, tokenType TokenType) refreshControl {
	token := config.GetTokens(tokenType)

	until := time.Until(getRefreshTime(token))
	log.Debugf("Waiting %v before refreshing token that expires at %s", until, token.ExpiresAt)

	return delay(ctx, until)
}

func tokenRefresh(ctx context.Context, client *irc.Client, config *ConfigManager, tokenType TokenType) refreshControl {
	log.Info("Refreshing token...")

	token := config.GetTokens(tokenType)
	newTokens, err := refreshTokens(config, token.RefreshToken)
	if err != nil {
		log.Errorf("Failed to refresh token: %v", err)
		return delay(ctx, 30*time.Second)
	}

	accessToken, refreshToken, expiresAt := newTokens.get()
	config.SetTokens(tokenType, accessToken, refreshToken, parseExpiresTime(expiresAt), token.UserID, token.Username)

	client.SetIRCToken(prefixToken(accessToken))

	log.Info("Token refreshed successfully")
	log.Debugf("New token expires at: %s", expiresAt)

	return refreshControlContinue
}

// getRefreshTime when 10 minutes are left (or immediately if already expired)
func getRefreshTime(token UserTokens) time.Time {
	if token.IsExpired() {
		return time.Now()
	}

	return token.ExpiresAt.Add(-10 * time.Minute)
}

func delay(ctx context.Context, d time.Duration) refreshControl {
	select {
	case <-ctx.Done():
		log.Info("delay stopping during wait")
		return refreshControlStop
	case <-time.After(d):
	}

	return refreshControlContinue
}

func tokenRefreshWatch(ctx context.Context, client *irc.Client, config *ConfigManager, tokenType TokenType) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Token refresh routine stopping")
			return
		default:
		}

		if tokenDelay(ctx, config, tokenType) == refreshControlStop {
			return
		}

		if tokenRefresh(ctx, client, config, tokenType) == refreshControlStop {
			return
		}
	}
}
