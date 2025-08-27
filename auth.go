package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/nicklaw5/helix/v2"
)

type Token struct{ helix.AccessCredentials }

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

		config := GetConfig()
		token, err := getUserToken(config, s.code)
		if err != nil {
			log.Errorf("Failed to get access token: %v", err)
			http.Error(w, fmt.Sprintf("Failed to get access token: %v", err), http.StatusInternalServerError)
			return
		}

		tokenStr, refresh, expires := token.get()

		expiresAt := parseExpiresTime(expires)
		config.SetTokens(tokenStr, refresh, expiresAt)

		tmpl := template.Must(template.New("callback").Parse(callbackTemplate))
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

func performOAuthFlow(config *ConfigManager) error {
	twitchConfig := config.Twitch()
	serverConfig := config.Server()

	redirect := fmt.Sprintf("http://localhost:%s/callback", serverConfig.OAuthPort)
	if serverConfig.VirtualHost != "" {
		redirect = fmt.Sprintf("https://%s/callback", serverConfig.VirtualHost)
	}

	client, err := helix.NewClient(&helix.Options{
		ClientID:    twitchConfig.ClientID,
		RedirectURI: redirect,
	})
	if err != nil {
		return fmt.Errorf("unable to set up helix client: %w", err)
	}

	authURL := client.GetAuthorizationURL(&helix.AuthorizationURLParams{
		ResponseType: "code",
		Scopes:       []string{"chat:edit", "chat:read", "whispers:read", "whispers:edit"},
	})

	s := server{
		listen: ":" + serverConfig.OAuthPort,
	}
	s.setupRoutes(authURL)

	if err := s.Start(); err != nil {
		return fmt.Errorf("unable to start OAuth server: %w", err)
	}

	if s.code == "" {
		return fmt.Errorf("no authorization code received")
	}

	return nil
}

func getUserToken(config *ConfigManager, code string) (*Token, error) {
	twitchConfig := config.Twitch()
	serverConfig := config.Server()

	redirect := fmt.Sprintf("http://localhost:%s/callback", serverConfig.OAuthPort)
	if serverConfig.VirtualHost != "" {
		redirect = fmt.Sprintf("https://%s/callback", serverConfig.VirtualHost)
	}

	client, err := helix.NewClient(&helix.Options{
		ClientID:     twitchConfig.ClientID,
		ClientSecret: twitchConfig.ClientSecret,
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

func refreshTokens(config *ConfigManager, refreshToken string) (*Token, error) {
	twitchConfig := config.Twitch()

	client, err := helix.NewClient(&helix.Options{
		ClientID:     twitchConfig.ClientID,
		ClientSecret: twitchConfig.ClientSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("refreshToken: unable to set up client: %w", err)
	}

	log.Debugf("Attempting to refresh token with refresh token: %s...", refreshToken[:min(len(refreshToken), 10)])

	r, err := client.RefreshUserAccessToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("refreshToken: unable to refresh token: %w", err)
	} else if r.ErrorStatus != 0 {
		return nil, fmt.Errorf("refreshToken: invalid response: %v - %s", r.ErrorStatus, r.ErrorMessage)
	}

	log.Debug("Token refresh successful")
	return &Token{r.Data}, nil
}

func parseExpiresTime(expires string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		log.Errorf("Failed to parse expires time: %v", err)
		return time.Now().Add(time.Hour) // Fallback to 1 hour from now
	}
	return t
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
