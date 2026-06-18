package main

import (
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Server holds dependencies for the HTTP handlers.
type Server struct {
	client Client
	cfg    *Config
	now    func() time.Time
}

func NewServer(client Client, cfg *Config) *Server {
	return &Server{client: client, cfg: cfg, now: time.Now}
}

func parseIntParam(values url.Values, key string, def int) int {
	raw := values.Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statusTypes := parseIntParam(q, "servicestatustypes", 28)
	serviceProps := parseIntParam(q, "serviceprops", 0)
	hostProps := parseIntParam(q, "hostprops", 0)
	callback := q.Get("callback")
	jsonMode := parseIntParam(q, "json", 1)

	now := s.now()
	_, off := now.Zone()
	isdst := 0
	if now.IsDST() {
		isdst = 1
	}

	env := Envelope{
		Version:    s.cfg.Version,
		Running:    0,
		ServerTime: now.Unix(),
		Created:    now.Unix(),
		LocalTime:  [2]int64{int64(isdst), int64(off)},
		Data:       []Service{},
	}

	// Only report running:1 with data when BOTH Zabbix calls succeed; any
	// failure degrades to running:0 / empty data (spec §8), so the front-end
	// never sees a partial snapshot (e.g. rows with blank hostnames).
	if problems, err := s.client.Problems(r.Context()); err == nil {
		triggerIDs := make([]string, 0, len(problems))
		for _, p := range problems {
			triggerIDs = append(triggerIDs, p.TriggerID)
		}
		if hosts, herr := s.client.Hostnames(r.Context(), triggerIDs); herr == nil {
			env.Running = 1
			rows := BuildServices(problems, hosts, now.Unix())
			env.Data = Filter(rows, statusTypes, serviceProps, hostProps)
		}
	}

	if jsonMode == 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_ = WritePlaintext(w, env.Data)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = WriteJSON(w, env, callback)
}
