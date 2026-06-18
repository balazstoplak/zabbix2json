package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Envelope is the top-level nagios2json response.
type Envelope struct {
	Version    uint32
	Running    int
	ServerTime int64
	Created    int64
	LocalTime  [2]int64
	Data       []Service
}

// jsonRow mirrors the nagios2json row field order exactly.
type jsonRow struct {
	LastStateChange int64  `json:"last_state_change"`
	PluginOutput    string `json:"plugin_output"`
	Status          string `json:"status"`
	Flags           int    `json:"flags"`
	Hostname        string `json:"hostname"`
	Service         string `json:"service"`
	HostAlive       int    `json:"host_alive"`
	ServicesTotal   int    `json:"services_total"`
	ServicesVisible int    `json:"services_visible"`
	Duration        string `json:"duration"`
}

type jsonEnvelope struct {
	Version    uint32    `json:"version"`
	Running    int       `json:"running"`
	ServerTime int64     `json:"servertime"`
	LocalTime  [2]int64  `json:"localtime"`
	Created    int64     `json:"created"`
	Data       []jsonRow `json:"data"`
}

// WriteJSON emits the envelope as compact JSON (optionally JSONP-wrapped),
// matching nagios2json: no HTML escaping, no trailing newline.
func WriteJSON(w io.Writer, env Envelope, callback string) error {
	rows := make([]jsonRow, 0, len(env.Data))
	for _, s := range env.Data {
		rows = append(rows, jsonRow{
			LastStateChange: s.LastStateChange,
			PluginOutput:    s.PluginOutput,
			Status:          StatusName(s.Status),
			Flags:           s.Flags,
			Hostname:        s.Hostname,
			Service:         s.Service,
			HostAlive:       s.HostAlive,
			ServicesTotal:   s.ServicesTotal,
			ServicesVisible: s.ServicesVisible,
			Duration:        FormatDuration(s.DurationSecs),
		})
	}
	payload := jsonEnvelope{
		Version: env.Version, Running: env.Running, ServerTime: env.ServerTime,
		LocalTime: env.LocalTime, Created: env.Created, Data: rows,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return err
	}
	body := bytes.TrimRight(buf.Bytes(), "\n")

	if callback != "" {
		if _, err := fmt.Fprintf(w, "%s(", callback); err != nil {
			return err
		}
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if callback != "" {
		if _, err := io.WriteString(w, ")"); err != nil {
			return err
		}
	}
	return nil
}

// WritePlaintext renders the nagios console table format.
func WritePlaintext(w io.Writer, services []Service) error {
	for _, s := range services {
		_, err := fmt.Fprintf(w, "%-25s | %-20s | %-8s | %-16s | \"%s\"\n",
			s.Hostname, s.Service, StatusName(s.Status), FormatDuration(s.DurationSecs), s.PluginOutput)
		if err != nil {
			return err
		}
	}
	return nil
}
