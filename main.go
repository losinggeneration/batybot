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

	"github.com/gempir/go-twitch-irc/v4"
	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

func prefixToken(token string) string {
	if strings.HasPrefix(token, "oauth:") {
		return token
	}

	return "oauth:" + token
}

func setup() (*twitch.Client, *ConfigManager) {
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

	if !config.HasValidTokens() {
		log.Info("Starting need to get toke...")

		if err := oauthCodeFlow(config); err != nil {
			log.Fatalf("Auth failed: %v", err)
		}

		log.Info("Authorization successful!")
	}

	accessToken, _, expiresAt := config.GetTokens()
	log.Debugf("Token expires at: %v", expiresAt)

	client := twitch.NewClient("batybot", prefixToken(accessToken))

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

type refreshControl int

const (
	refreshControlStop     = 1
	refreshControlContinue = 2
)

func tokenDelay(ctx context.Context, config *ConfigManager) refreshControl {
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
		return refreshControlStop
	case <-time.After(until):
	}

	return refreshControlContinue
}

func tokenRefresh(ctx context.Context, client *twitch.Client, config *ConfigManager) refreshControl {
	log.Info("Refreshing token...")

	_, refreshToken, _ := config.GetTokens()
	newTokens, err := refreshTokens(config, refreshToken)
	if err != nil {
		log.Errorf("Failed to refresh token: %v", err)
		select {
		case <-ctx.Done():
			return refreshControlStop
		case <-time.After(30 * time.Second):
			return refreshControlContinue
		}
	}

	accessToken, refreshToken, expiresAt := newTokens.get()
	config.SetTokens(accessToken, refreshToken, parseExpiresTime(expiresAt))

	client.SetIRCToken(prefixToken(accessToken))

	log.Info("Token refreshed successfully")
	log.Debugf("New token expires at: %s", expiresAt)

	return refreshControlContinue
}

func tokenRefreshWatch(ctx context.Context, client *twitch.Client, config *ConfigManager) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Token refresh routine stopping")
			return
		default:
		}

		if tokenDelay(ctx, config) == refreshControlStop {
			return
		}

		if tokenRefresh(ctx, client, config) == refreshControlStop {
			return
		}
	}
}
