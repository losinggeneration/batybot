package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nicklaw5/helix/v2"
)

type Token struct{ helix.AccessCredentials }

type server struct {
	http.Server

	listen string
	code   string
}

var (
	listen   = ":8080"
	redirect = fmt.Sprintf("http://localhost%s", listen)
)

func init() {
	if vhost := os.Getenv("VIRTUAL_HOST"); vhost != "" {
		redirect = fmt.Sprintf("https://%s", vhost)
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.code = q.Get("code") // scope is also available, but I don't think it's needed
	s.Shutdown(r.Context())
}

func (s *server) Start() error {
	s.Addr = s.listen
	s.Handler = s

	return fmt.Errorf("unable to start server: %w", s.ListenAndServe())
}

func (t Token) get() (token, refresh, expires string) {
	token = t.AccessToken
	if !strings.HasPrefix(token, "oauth:") {
		token = "oauth:" + token
	}

	refresh = t.RefreshToken
	expires = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second).Format(time.RFC3339Nano)
	// expires = strconv.FormatInt(int64(t.ExpiresIn), 10)

	return token, refresh, expires
}

func authCode() (string, error) {
	client, err := helix.NewClient(&helix.Options{
		ClientID:    os.Getenv("TWITCH_CLIENT_ID"),
		RedirectURI: redirect,
	})
	if err != nil {
		return "", fmt.Errorf("authCode: unable to set up client: %w", err)
	}

	url := client.GetAuthorizationURL(&helix.AuthorizationURLParams{
		ResponseType: "code",
		Scopes:       []string{"chat:edit", "chat:read", "whispers:read", "whispers:edit"},
	})

	log.Info(url)

	s := server{
		listen: listen,
	}
	if err := s.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return "", fmt.Errorf("authCode: unable to start server: %w", err)
	}

	return s.code, nil
}

func getUserToken(code string) (*Token, error) {
	client, err := helix.NewClient(&helix.Options{
		ClientID:     os.Getenv("TWITCH_CLIENT_ID"),
		ClientSecret: os.Getenv("TWITCH_CLIENT_SECRET"),
		RedirectURI:  redirect,
	})
	if err != nil {
		return nil, fmt.Errorf("getUserToken: unable to set up client: %w", err)
	}

	r, err := client.RequestUserAccessToken(code)
	if err != nil {
		return nil, fmt.Errorf("getUserToken: unable to get user token: %w", err)
	} else if r.ErrorStatus != 0 {
		return nil, fmt.Errorf("getUserToken: invalid response: %v", r.ErrorStatus)
	}

	return &Token{r.Data}, nil
}

func getToken() (*Token, error) {
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
	client, err := helix.NewClient(&helix.Options{
		ClientID:     os.Getenv("TWITCH_CLIENT_ID"),
		ClientSecret: os.Getenv("TWITCH_CLIENT_SECRET"),
	})
	if err != nil {
		return nil, fmt.Errorf("refreshToken: unable to set up client: %w", err)
	}

	r, err := client.RefreshUserAccessToken(refresh)
	if err != nil {
		return nil, fmt.Errorf("refreshToken: unable to refresh token: %w", err)
	} else if r.ErrorStatus != 0 {
		return nil, fmt.Errorf("refreshToken: invalid response: %v - %s", r.ErrorStatus, r.ErrorMessage)
	}

	return &Token{r.Data}, nil
}
