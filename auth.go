package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	helix "github.com/nicklaw5/helix/v2"
)

type Token struct{ helix.AccessCredentials }

type server struct {
	http.Server
	mux          *http.ServeMux
	listen       string
	code         string
	done         chan bool
	tokenType    TokenType
	expectedUser string
}

//go:embed *.html.tmpl
var embedFS embed.FS

func loadEmbedFs(name string) (string, error) {
	b, err := embedFS.ReadFile(name + ".html.tmpl")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (s *server) error(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	errMsg := q.Get("error")
	if errMsg == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

	log.Errorf("Auth error: %s - %s", errMsg, q.Get("error_description"))

	data := struct {
		Message     string
		Description string
	}{
		Message:     errMsg,
		Description: q.Get("error_description"),
	}

	s.showTemplate(w, "error", "error", data)
}

func (s *server) indexHandler(authURL, userType, expectedUser string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errMsg := q.Get("error"); errMsg != "" {
			http.Redirect(w, r, "/error?"+r.URL.Query().Encode(), http.StatusSeeOther)
			return
		}

		data := struct {
			AuthURL      string
			UserType     string
			ExpectedUser string
		}{
			AuthURL:      authURL,
			UserType:     userType,
			ExpectedUser: expectedUser,
		}

		s.showTemplate(w, "index", "index", data)
	}
}

func (s *server) showTemplate(w http.ResponseWriter, filename, name string, data any) {
	file, err := loadEmbedFs(filename)
	if err != nil {
		panic(err)
	}

	tmpl := template.Must(template.New(name).Parse(file))

	if err := tmpl.Execute(w, data); err != nil {
		log.Errorf("Unable to write response: %s", err)
	}
}

func (s *server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintln(w, "OK"); err != nil {
		log.Errorf("Unable to write response: %s", err)
	}
}

func (s *server) callbackHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if errMsg := q.Get("error"); errMsg != "" {
		http.Redirect(w, r, "/error?"+r.URL.Query().Encode(), http.StatusSeeOther)
		return
	}

	s.code = q.Get("code")
	if s.code == "" {
		log.Errorf("Failed to get code: %v", r.URL.Query().Encode())
		q := r.URL.Query()
		q.Add("error", "No authorization code received")
		http.Redirect(w, r, "/error?"+q.Encode(), http.StatusSeeOther)
		return
	}

	config := GetConfig()
	token, user, err := getUserToken(config, s.code)
	if err != nil {
		log.Errorf("Failed to get access token: %v", err)
		q := r.URL.Query()
		q.Add("error", err.Error())
		http.Redirect(w, r, "/error?"+q.Encode(), http.StatusSeeOther)
		return
	}

	if user.Login != s.expectedUser {
		log.Errorf("Wrong user authorized: expected %s, got %s", s.expectedUser, user.Login)
		q := r.URL.Query()
		q.Add("error", fmt.Sprintf("Wrong user: expected %s, got %s", s.expectedUser, user.Login))
		http.Redirect(w, r, "/error?"+q.Encode(), http.StatusSeeOther)
		return
	}

	tokenStr, refresh, expires := token.get()
	expiresAt := parseExpiresTime(expires)

	config.SetTokens(s.tokenType, tokenStr, refresh, expiresAt, user.ID, user.Login)
	log.Infof("Tokens(%d) stored for user: %s", s.tokenType, user.Login)

	data := struct {
		Token      string
		Refresh    string
		Expires    string
		ExpiresRaw string
		UserType   string
		Username   string
	}{
		Token:      tokenStr,
		Refresh:    refresh,
		Expires:    expiresAt.Format(time.DateTime),
		ExpiresRaw: expires,
		UserType:   userType(s.tokenType),
		Username:   user.Login,
	}

	s.showTemplate(w, "callback", "callback", data)

	go func() {
		time.Sleep(2 * time.Second) // Give time for the response to be sent
		s.done <- true
	}()
}

func (s *server) setupRoutes(authURL, userType, expectedUser string) {
	s.mux = http.NewServeMux()

	s.mux.HandleFunc("/", s.indexHandler(authURL, userType, expectedUser))
	s.mux.HandleFunc("/health", s.healthHandler)
	s.mux.HandleFunc("/error", s.error)
	s.mux.HandleFunc("/callback", s.callbackHandler)

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

	log.Debugf("Server started at http://localhost%s", s.listen)
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

func oauthClientFlow(config *ConfigManager) error {
	token, err := getAppToken(config)
	if err != nil {
		return err
	}

	tokenStr, refresh, expires := token.get()

	expiresAt := parseExpiresTime(expires)
	config.SetBotTokens(tokenStr, refresh, expiresAt, config.Twitch().User, config.Twitch().User)

	return nil
}

func getAppToken(config *ConfigManager) (*Token, error) {
	twitchConfig := config.Twitch()

	client, err := helix.NewClient(&helix.Options{
		ClientID:     twitchConfig.ClientID,
		ClientSecret: twitchConfig.ClientSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("getAppToken: unable to set up helix client: %w", err)
	}

	r, err := client.RequestAppAccessToken(config.Twitch().Scopes.Bot)
	if err != nil {
		return nil, fmt.Errorf("getAppToken: unable to get user token: %w", err)
	} else if r.ErrorStatus != 0 {
		return nil, fmt.Errorf("getAppToken: invalid response: %v - %s", r.ErrorStatus, r.ErrorMessage)
	}

	return &Token{r.Data}, nil
}

func userType(tokenType TokenType) string {
	if tokenType == BroadcasterTokenType {
		return "broadcaster"
	}

	return "bot"
}

func oauthCodeFlow(config *ConfigManager, tokenType TokenType) error {
	twitchConfig := config.Twitch()
	serverConfig := config.Server()

	redirect := fmt.Sprintf("http://localhost:%s/callback", serverConfig.OAuthPort)
	if serverConfig.VirtualHost != "" {
		redirect = fmt.Sprintf("https://%s/callback", serverConfig.VirtualHost)
	}

	// assume bot, unless it's not
	scopes := twitchConfig.Scopes.Bot
	userType := userType(tokenType)
	expectedUser := twitchConfig.User

	if tokenType == BroadcasterTokenType {
		scopes = twitchConfig.Scopes.Broadcaster
		expectedUser = twitchConfig.Broadcaster
	}

	log.Infof("Starting OAuth flow for %s user (%s)", userType, expectedUser)

	client, err := helix.NewClient(&helix.Options{
		ClientID:    twitchConfig.ClientID,
		RedirectURI: redirect,
	})
	if err != nil {
		return fmt.Errorf("unable to set up helix client: %w", err)
	}

	authURL := client.GetAuthorizationURL(&helix.AuthorizationURLParams{
		ResponseType: "code",
		Scopes:       scopes,
		State:        string(rune(tokenType)), // Pass auth type as state
	})

	s := server{
		listen:       ":" + serverConfig.OAuthPort,
		tokenType:    tokenType,
		expectedUser: expectedUser,
	}
	s.setupRoutes(authURL, userType, expectedUser)

	if err := s.Start(); err != nil {
		return fmt.Errorf("unable to start OAuth server: %w", err)
	}

	if s.code == "" {
		return fmt.Errorf("no authorization code received")
	}

	return nil
}

func oauthFlow(config *ConfigManager) error {
	log.Info("Starting OAuth flow...")

	if !config.IsValidTokens() {
		log.Info("All tokens are valid, no authentication needed")
		return nil
	}

	if !config.IsValidBotTokens() {
		log.Info("Bot authentication required...")
		if err := oauthCodeFlow(config, BotTokenType); err != nil {
			return fmt.Errorf("bot auth failed: %w", err)
		}
		log.Info("Bot authentication successful!")
	}

	if !config.IsValidBroadcasterTokens() {
		log.Info("Broadcaster authentication required...")
		if err := oauthCodeFlow(config, BroadcasterTokenType); err != nil {
			return fmt.Errorf("broadcaster auth failed: %w", err)
		}
		log.Info("Broadcaster authentication successful!")
	}

	return nil
}

func getUserToken(config *ConfigManager, code string) (*Token, *helix.User, error) {
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
		return nil, nil, fmt.Errorf("getUserToken: unable to set up client: %w", err)
	}

	r, err := client.RequestUserAccessToken(code)
	if err != nil {
		return nil, nil, fmt.Errorf("getUserToken: unable to get user token: %w", err)
	} else if r.ErrorStatus != 0 {
		return nil, nil, fmt.Errorf("getUserToken: invalid response: %v - %s", r.ErrorStatus, r.ErrorMessage)
	}

	client.SetUserAccessToken(r.Data.AccessToken)
	user, err := client.GetUsers(&helix.UsersParams{})
	if err != nil {
		return nil, nil, fmt.Errorf("getUserToken: unable to get user info: %w", err)
	}

	if len(user.Data.Users) == 0 {
		return nil, nil, fmt.Errorf("getUserToken: no user data returned")
	}

	return &Token{r.Data}, &user.Data.Users[0], nil
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
