package main

import (
	"crypto/subtle"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/shopspring/decimal"
)

type dashboardServer struct {
	http.Server
	mux    *http.ServeMux
	db     *DB
	config *ConfigManager
}

// basicAuth middleware
func (s *dashboardServer) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		password := s.config.Server().DashboardPassword
		if password == "" {
			next(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok {
			s.unauthorized(w)
			return
		}

		userMatch := subtle.ConstantTimeCompare([]byte(user), []byte("admin"))
		passMatch := subtle.ConstantTimeCompare([]byte(pass), []byte(password))

		if userMatch != 1 || passMatch != 1 {
			s.unauthorized(w)
			return
		}

		next(w, r)
	}
}

func (s *dashboardServer) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Batybot Dashboard"`)
	http.Error(w, "Authentication required", http.StatusUnauthorized)
}

func (s *dashboardServer) indexHandler(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.getUsers()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	totals, err := s.db.getTotals()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	total := decimal.NewFromFloat(float64(totals.Bits) / 100)
	total = total.Add(totals.Tips)
	total = total.Add(decimal.NewFromInt(5*totals.Tier1 + 10*totals.Tier2 + 25*totals.Tier3))

	data := struct {
		Users  map[string]UserTotals
		Totals UserTotals
		Total  decimal.Decimal
	}{
		Users:  users,
		Totals: *totals,
		Total:  total,
	}

	showTemplate(w, "dashboard", "dashboard", data)
}

func (s *dashboardServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintln(w, "OK"); err != nil {
		log.Errorf("Unable to write response: %s", err)
	}
}

func (s *dashboardServer) Start() error {
	s.mux = http.NewServeMux()

	s.mux.HandleFunc("/", s.basicAuth(s.indexHandler))
	s.mux.HandleFunc("/health", s.healthHandler)

	staticFS, err := fs.Sub(embedFS, "static")
	if err != nil {
		return fmt.Errorf("failed to load static files: %w", err)
	}
	s.mux.Handle("/static/", s.basicAuth(func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))).ServeHTTP(w, r)
	}))

	s.Handler = s.mux
	s.Addr = ":8080"
	return s.ListenAndServe()
}
