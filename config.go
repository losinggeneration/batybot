package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	koanfjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Twitch  TwitchConfig  `koanf:"twitch"`
	Server  ServerConfig  `koanf:"server"`
	Bot     BotConfig     `koanf:"bot"`
	Logging LoggingConfig `koanf:"logging"`
}

type TwitchConfig struct {
	ClientID     string `koanf:"client_id" validate:"required"`
	ClientSecret string `koanf:"client_secret" validate:"required"`
	User         string `koanf:"user" validate:"required"`
	Channel      string `koanf:"channel" validate:"required"`
	Broadcaster  string `koanf:"broadcaster" validate:"required"`
	Scopes       Scopes `koanf:"scopes"`
}

type Scopes struct {
	Bot         []string `koanf:"bot"`
	Broadcaster []string `koanf:"broadcaster"`
}

type ServerConfig struct {
	OAuthPort   string `koanf:"oauth_port" validate:"required"`
	VirtualHost string `koanf:"virtual_host"`
}

type BotConfig struct {
	Verified bool `koanf:"verified"`
}

type LoggingConfig struct {
	Level string `koanf:"level"`
}

type TokenStore struct {
	mu                sync.RWMutex
	BotTokens         UserTokens `json:"bot_tokens"`
	BroadcasterTokens UserTokens `json:"broadcaster_tokens"`
}

type UserTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
}

type ConfigManager struct {
	config *Config
	tokens *TokenStore
	koanf  *koanf.Koanf
}

type TokenType int

const (
	BotTokenType TokenType = iota
	BroadcasterTokenType
)

var (
	globalConfig *ConfigManager
	configOnce   sync.Once
)

// InitConfig initializes the global configuration manager
func InitConfig(cfg string) (*ConfigManager, error) {
	var err error
	configOnce.Do(func() {
		globalConfig, err = newConfigManager(cfg)
	})
	return globalConfig, err
}

// GetConfig returns the global configuration manager
func GetConfig() *ConfigManager {
	if globalConfig == nil {
		panic("config not initialized - call InitConfig() first")
	}
	return globalConfig
}

func newConfigManager(cfg string) (*ConfigManager, error) {
	k := koanf.New(".")

	defaults := Config{
		Twitch: TwitchConfig{
			Scopes: Scopes{
				Bot: []string{
					"chat:edit",
					"chat:read",
					"user:bot",
					"user:read:chat",
					"user:write:chat",
					"whispers:edit",
					"whispers:read",
				},
				Broadcaster: []string{
					"bits:read",
					"channel:bot",
					"channel:read:subscriptions",
					"moderator:read:followers",
					"user:bot",
					"user:read:chat",
				},
			},
		},
		Server: ServerConfig{
			OAuthPort: "8080",
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}

	if err := k.Load(structs.Provider(defaults, "koanf"), nil); err != nil {
		return nil, fmt.Errorf("error loading defaults: %w", err)
	}

	configFiles := []string{"config.yaml", "config.yml", "config.json"}
	if cfg != "" {
		configFiles = []string{cfg}
	}

	for _, configFile := range configFiles {
		var parser koanf.Parser
		if configFile[len(configFile)-4:] == "json" {
			parser = koanfjson.Parser()
		} else {
			parser = yaml.Parser()
		}

		if err := k.Load(file.Provider(configFile), parser); err == nil {
			log.Debugf("Loaded configuration from %s", configFile)
			break
		}
	}

	if err := k.Load(env.Provider("BATYBOT_", ".", func(s string) string {
		return map[string]string{
			"BATYBOT_TWITCH_CLIENT_ID":     "twitch.client_id",
			"BATYBOT_TWITCH_CLIENT_SECRET": "twitch.client_secret",
			"BATYBOT_TWITCH_USER":          "twitch.user",
			"BATYBOT_TWITCH_CHANNEL":       "twitch.channel",
			"BATYBOT_TWITCH_BROADCASTER":   "twitch.broadcaster",
			"BATYBOT_OAUTH_PORT":           "server.oauth_port",
			"BATYBOT_VIRTUAL_HOST":         "server.virtual_host",
			"BATYBOT_BOT_VERIFIED":         "bot.verified",
			"BATYBOT_LOG_LEVEL":            "logging.level",
		}[s]
	}), nil); err != nil {
		return nil, fmt.Errorf("error loading environment variables: %w", err)
	}

	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	tokens := &TokenStore{}

	if err := tokens.LoadFromFile("tokens.json"); err != nil {
		log.Debug("No existing token file found or failed to load")
	}

	return &ConfigManager{
		config: &config,
		tokens: tokens,
		koanf:  k,
	}, nil
}

func (u UserTokens) IsExpired() bool {
	// Consider token expired if less than 10 minutes remain
	return time.Now().After(u.ExpiresAt.Add(-10 * time.Minute))
}

func (u UserTokens) isValid() bool {
	return u.AccessToken != "" &&
		u.RefreshToken != "" &&
		!u.IsExpired()
}

// validate required configuration fields
func (c Config) validate() error {
	if c.Twitch.ClientID == "" {
		return fmt.Errorf("twitch.client_id is required")
	}
	if c.Twitch.User == "" {
		return fmt.Errorf("twitch.user is required")
	}
	if c.Twitch.Channel == "" {
		return fmt.Errorf("twitch.channel is required")
	}
	if c.Twitch.Broadcaster == "" {
		return fmt.Errorf("twitch.broadcaster is required")
	}

	tokens := &TokenStore{}
	if err := tokens.LoadFromFile("tokens.json"); err != nil {
		log.Infof("tokens.json not read: %v", err)
	}

	if (!tokens.BotTokens.isValid() || !tokens.BroadcasterTokens.isValid()) && c.Twitch.ClientSecret == "" {
		return fmt.Errorf("twitch.client_secret is required for OAuth authorization")
	}

	return nil
}

func (cm *ConfigManager) Twitch() TwitchConfig {
	return cm.config.Twitch
}

func (cm *ConfigManager) Server() ServerConfig {
	return cm.config.Server
}

func (cm *ConfigManager) Bot() BotConfig {
	return cm.config.Bot
}

func (cm *ConfigManager) Logging() LoggingConfig {
	return cm.config.Logging
}

func (cm *ConfigManager) GetTokens(tokenType TokenType) UserTokens {
	cm.tokens.mu.RLock()
	defer cm.tokens.mu.RUnlock()

	switch tokenType {
	case BotTokenType:
		return cm.tokens.BotTokens
	case BroadcasterTokenType:
		return cm.tokens.BroadcasterTokens
	}

	log.Panicf("Invalid TokenType: %v", tokenType)
	return UserTokens{}
}

func (cm *ConfigManager) GetBotTokens() UserTokens {
	return cm.GetTokens(BotTokenType)
}

func (cm *ConfigManager) GetBroadcasterTokens() UserTokens {
	return cm.GetTokens(BroadcasterTokenType)
}

func (cm *ConfigManager) SetTokens(tokenType TokenType, accessToken, refreshToken string, expiresAt time.Time, userID, username string) {
	cm.tokens.mu.Lock()
	defer cm.tokens.mu.Unlock()

	var token *UserTokens
	switch tokenType {
	case BotTokenType:
		token = &cm.tokens.BotTokens
	case BroadcasterTokenType:
		token = &cm.tokens.BroadcasterTokens
	default:
		log.Panicf("Invalid TokenType: %v", tokenType)
	}

	token.AccessToken = accessToken
	token.RefreshToken = refreshToken
	token.ExpiresAt = expiresAt
	token.UserID = userID
	token.Username = username

	if err := cm.tokens.saveToFile("tokens.json"); err != nil {
		log.Warnf("Failed to save tokens to file: %v", err)
	}
}

func (cm *ConfigManager) SetBotTokens(accessToken, refreshToken string, expiresAt time.Time, userID, username string) {
	cm.SetTokens(BotTokenType, accessToken, refreshToken, expiresAt, userID, username)
}

func (cm *ConfigManager) SetBroadcasterTokens(accessToken, refreshToken string, expiresAt time.Time, userID, username string) {
	cm.SetTokens(BroadcasterTokenType, accessToken, refreshToken, expiresAt, userID, username)
}

func (cm *ConfigManager) IsValidTokens() bool {
	return cm.IsValidBotTokens() && cm.IsValidBroadcasterTokens()
}

func (cm *ConfigManager) IsValidBotTokens() bool {
	cm.tokens.mu.RLock()
	defer cm.tokens.mu.RUnlock()
	return cm.tokens.BotTokens.isValid()
}

func (cm *ConfigManager) IsValidBroadcasterTokens() bool {
	cm.tokens.mu.RLock()
	defer cm.tokens.mu.RUnlock()
	return cm.tokens.BroadcasterTokens.isValid()
}

func (ts *TokenStore) LoadFromFile(filename string) error {
	data, err := readJSONFile(filename)
	if err != nil {
		return err
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	return unmarshalJSON(data, ts)
}

func (ts *TokenStore) saveToFile(filename string) error {
	data, err := marshalJSON(ts)
	if err != nil {
		return err
	}

	return writeJSONFile(filename, data)
}

func readJSONFile(filename string) ([]byte, error) {
	return readFile(filename)
}

func writeJSONFile(filename string, data []byte) error {
	return writeFile(filename, data, 0600)
}

func marshalJSON(v any) ([]byte, error) {
	return jsonMarshalIndent(v, "", "  ")
}

func unmarshalJSON(data []byte, v any) error {
	return jsonUnmarshal(data, v)
}

var (
	readFile          = readFileImpl
	writeFile         = writeFileImpl
	jsonMarshalIndent = jsonMarshalIndentImpl
	jsonUnmarshal     = jsonUnmarshalImpl
)

func readFileImpl(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

func writeFileImpl(filename string, data []byte, perm int) error {
	if dir := filepath.Dir(filename); dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return os.WriteFile(filename, data, os.FileMode(perm))
}

func jsonMarshalIndentImpl(v any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}

func jsonUnmarshalImpl(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// String returns a safe string representation of the config (without secrets)
func (cm *ConfigManager) String() string {
	return fmt.Sprintf("Config{Twitch.User: %s, Twitch.Channel: %s, Twitch.Broadcaster: %s, Server.OAuthPort: %s, Bot.Verified: %t}",
		cm.config.Twitch.User,
		cm.config.Twitch.Channel,
		cm.config.Twitch.Broadcaster,
		cm.config.Server.OAuthPort,
		cm.config.Bot.Verified)
}
