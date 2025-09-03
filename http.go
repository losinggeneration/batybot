package main

import (
	"fmt"
	"net/http"

	"github.com/shopspring/decimal"
)

type dashboardServer struct {
	http.Server
	mux *http.ServeMux
	db  *DB
}

func (s *dashboardServer) indexHandler(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.getUsers()
	if err != nil {
		return
	}

	totals, err := s.db.getTotals()
	if err != nil {
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

	s.mux.HandleFunc("/", s.indexHandler)
	s.mux.HandleFunc("/health", s.healthHandler)

	s.Handler = s.mux
	s.Addr = ":8080"
	return s.ListenAndServe()
}
