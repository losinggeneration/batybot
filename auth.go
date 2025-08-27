package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nicklaw5/helix/v2"
)

type Token struct{ helix.AccessCredentials }

type TokenStore struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type server struct {
	http.Server
	mux    *http.ServeMux
	listen string
	code   string
	done   chan bool
}

//go:embed index.html.gotmpl callback.html.gotmpl
var embedFS embed.FS

var indexTemplate, callbackTemplate string

var (
	listen    = ":8080"
	redirect  = fmt.Sprintf("http://localhost%s/callback", listen)
	tokenFile = "tokens.json" // File to persist tokens
)

func init() {
	if b, err := embedFS.ReadFile("index.html.gotmpl"); err != nil {
		panic(err)
	} else {
		indexTemplate = string(b)
	}

	if b, err := embedFS.ReadFile("callback.html.gotmpl"); err != nil {
		panic(err)
	} else {
		callbackTemplate = string(b)
	}

	if vhost := os.Getenv("VIRTUAL_HOST"); vhost != "" {
		redirect = fmt.Sprintf("https://%s/callback", vhost)
	}

	if port := os.Getenv("OAUTH_PORT"); port != "" {
		listen = ":" + port
		if vhost := os.Getenv("VIRTUAL_HOST"); vhost == "" {
			redirect = fmt.Sprintf("http://localhost:%s/callback", port)
		}
	}
}

// loadTokensFromFile attempts to load tokens from a file
func loadTokensFromFile() (token, refresh, expires string, err error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", "", "", err
	}

	var store TokenStore
	if err := json.Unmarshal(data, &store); err != nil {
		return "", "", "", err
	}

	if time.Now().After(store.ExpiresAt.Add(-10 * time.Minute)) {
		return "", "", "", fmt.Errorf("stored token is expired")
	}

	token = store.AccessToken
	refresh = store.RefreshToken
	expires = store.ExpiresAt.Format(time.RFC3339Nano)

	log.Info("Loaded tokens from file")
	return token, refresh, expires, nil
}

// saveTokensToFile saves tokens to a file for persistence
func saveTokensToFile(token, refresh, expires string) error {
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return err
	}

	store := TokenStore{
		AccessToken:  token,
		RefreshToken: refresh,
		ExpiresAt:    expiresAt,
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	if dir := filepath.Dir(tokenFile); dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}

	if err := os.WriteFile(tokenFile, data, 0600); err != nil {
		return err
	}

	log.Info("Tokens saved to file")
	return nil
}

func (s *server) setupRoutes(authURL string) {
	s.mux = http.NewServeMux()

	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.New("index").Parse(indexTemplate))
		data := struct {
			AuthURL string
		}{
			AuthURL: authURL,
		}

		if err := tmpl.Execute(w, data); err != nil {
			log.Errorf("Unable to write response: %s", err)
		}
	})

	s.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := fmt.Fprintln(w, "OK"); err != nil {
			log.Errorf("Unable to write response: %s", err)
		}
	})

	s.mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if errMsg := q.Get("error"); errMsg != "" {
			log.Errorf("OAuth error: %s - %s", errMsg, q.Get("error_description"))
			http.Error(w, fmt.Sprintf("Authorization failed: %s - %s", errMsg, q.Get("error_description")), http.StatusBadRequest)
			return
		}

		s.code = q.Get("code")
		if s.code == "" {
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			return
		}

		token, err := getUserToken(s.code)
		if err != nil {
			log.Errorf("Failed to get access token: %v", err)
			http.Error(w, fmt.Sprintf("Failed to get access token: %v", err), http.StatusInternalServerError)
			return
		}

		tokenStr, refresh, expires := token.get()

		if err := os.Setenv("TWITCH_TOKEN", tokenStr); err != nil {
			log.Errorf("Unable to set environment variable %q: %s", "TWITCH_TOKEN", err)
		}
		if err := os.Setenv("TWITCH_REFRESH", refresh); err != nil {
			log.Errorf("Unable to set environment variable %q: %s", "TWITCH_REFRESH", err)
		}
		if err := os.Setenv("TWITCH_EXPIRES", expires); err != nil {
			log.Errorf("Unable to set environment variable %q: %s", "TWITCH_EXPIRES", err)
		}

		if err := saveTokensToFile(tokenStr, refresh, expires); err != nil {
			log.Warnf("Failed to save tokens to file: %v", err)
		}

		tmpl := template.Must(template.New("callback").Parse(callbackTemplate))

		expiresAt, _ := time.Parse(time.RFC3339Nano, expires)
		data := struct {
			Token      string
			Refresh    string
			Expires    string
			ExpiresRaw string
		}{
			Token:      tokenStr,
			Refresh:    refresh,
			Expires:    expiresAt.Format("January 2, 2006 at 3:04 PM MST"),
			ExpiresRaw: expires,
		}

		if err := tmpl.Execute(w, data); err != nil {
			log.Errorf("Unable to write response: %s", err)
		}

		go func() {
			time.Sleep(2 * time.Second) // Give time for the response to be sent
			s.done <- true
		}()
	})

	s.Handler = s.mux
}

func (s *server) Start() error {
	s.Addr = s.listen
	s.done = make(chan bool, 1)

	go func() {
		if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("Server error: %v", err)
		}
	}()

	log.Infof("OAuth server started at http://localhost%s", s.listen)
	log.Infof("Open your browser and navigate to http://localhost%s to authorize the bot", s.listen)

	<-s.done

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.Shutdown(ctx)
}

func (t Token) get() (token, refresh, expires string) {
	token = t.AccessToken
	refresh = t.RefreshToken
	expires = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second).Format(time.RFC3339Nano)

	return token, refresh, expires
}

func authCode() (string, error) {
	clientID := os.Getenv("TWITCH_CLIENT_ID")
	if clientID == "" {
		return "", fmt.Errorf("TWITCH_CLIENT_ID environment variable is required")
	}

	client, err := helix.NewClient(&helix.Options{
		ClientID:    clientID,
		RedirectURI: redirect,
	})
	if err != nil {
		return "", fmt.Errorf("authCode: unable to set up client: %w", err)
	}

	authURL := client.GetAuthorizationURL(&helix.AuthorizationURLParams{
		ResponseType: "code",
		Scopes:       []string{"chat:edit", "chat:read", "whispers:read", "whispers:edit"},
	})

	s := server{
		listen: listen,
	}
	s.setupRoutes(authURL)

	if err := s.Start(); err != nil {
		return "", fmt.Errorf("authCode: unable to start server: %w", err)
	}

	if s.code == "" {
		return "", fmt.Errorf("no authorization code received")
	}

	return s.code, nil
}

func getUserToken(code string) (*Token, error) {
	clientID := os.Getenv("TWITCH_CLIENT_ID")
	clientSecret := os.Getenv("TWITCH_CLIENT_SECRET")

	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("TWITCH_CLIENT_ID and TWITCH_CLIENT_SECRET environment variables are required")
	}

	client, err := helix.NewClient(&helix.Options{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirect,
	})
	if err != nil {
		return nil, fmt.Errorf("getUserToken: unable to set up client: %w", err)
	}

	r, err := client.RequestUserAccessToken(code)
	if err != nil {
		return nil, fmt.Errorf("getUserToken: unable to get user token: %w", err)
	} else if r.ErrorStatus != 0 {
		return nil, fmt.Errorf("getUserToken: invalid response: %v - %s", r.ErrorStatus, r.ErrorMessage)
	}

	return &Token{r.Data}, nil
}

// getToken tries token file first, then OAuth
func getToken() (*Token, error) {
	if token, refresh, expires, err := loadTokensFromFile(); err == nil {
		log.Info("Using tokens from file")

		if err := os.Setenv("TWITCH_TOKEN", token); err != nil {
			return nil, err
		}
		if err := os.Setenv("TWITCH_REFRESH", refresh); err != nil {
			return nil, err
		}
		if err := os.Setenv("TWITCH_EXPIRES", expires); err != nil {
			return nil, err
		}

		expiresAt, _ := time.Parse(time.RFC3339Nano, expires)
		expiresIn := int(time.Until(expiresAt).Seconds())

		return &Token{
			helix.AccessCredentials{
				AccessToken:  token,
				RefreshToken: refresh,
				ExpiresIn:    expiresIn,
			},
		}, nil
	}

	log.Info("No valid token file found, starting OAuth flow")

	code, err := authCode()
	if err != nil {
		return nil, fmt.Errorf("getToken: unable to get auth code: %w", err)
	}

	token, err := getUserToken(code)
	if err != nil {
		return nil, fmt.Errorf("getToken: unable to get user token: %w", err)
	}

	return token, nil
}

func refreshToken(refresh string) (*Token, error) {
	clientID := os.Getenv("TWITCH_CLIENT_ID")
	clientSecret := os.Getenv("TWITCH_CLIENT_SECRET")

	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("TWITCH_CLIENT_ID and TWITCH_CLIENT_SECRET environment variables are required")
	}

	client, err := helix.NewClient(&helix.Options{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("refreshToken: unable to set up client: %w", err)
	}

	log.Debugf("Attempting to refresh token with refresh token: %s...", refresh[:min(len(refresh), 10)])

	r, err := client.RefreshUserAccessToken(refresh)
	if err != nil {
		return nil, fmt.Errorf("refreshToken: unable to refresh token: %w", err)
	} else if r.ErrorStatus != 0 {
		return nil, fmt.Errorf("refreshToken: invalid response: %v - %s", r.ErrorStatus, r.ErrorMessage)
	}

	log.Debug("Token refresh successful")

	token := &Token{r.Data}
	tokenStr, refreshStr, expires := token.get()
	if err := saveTokensToFile(tokenStr, refreshStr, expires); err != nil {
		log.Warnf("Failed to update saved tokens: %v", err)
	}

	return token, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
