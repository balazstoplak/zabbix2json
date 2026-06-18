package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type rpcReq struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

func TestHTTPClientProblems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header: %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req rpcReq
		json.Unmarshal(body, &req)
		if req.Method != "problem.get" {
			t.Errorf("method: %q", req.Method)
		}
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":[
			{"eventid":"55","objectid":"t9","name":"CPU","severity":"4","clock":"100","acknowledged":"1","suppressed":"0","opdata":"9.1"}
		]}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	probs, err := c.Problems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 1 {
		t.Fatalf("want 1 problem, got %d", len(probs))
	}
	p := probs[0]
	if p.EventID != "55" || p.TriggerID != "t9" || p.Severity != 4 || p.Clock != 100 {
		t.Errorf("parsed wrong: %+v", p)
	}
	if !p.Acknowledged || p.Suppressed || p.Opdata != "9.1" {
		t.Errorf("bool/opdata parse wrong: %+v", p)
	}
}

func TestHTTPClientHostnames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":[
			{"triggerid":"t9","hosts":[{"name":"web01"}]}
		]}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	m, err := c.Hostnames(context.Background(), []string{"t9"})
	if err != nil {
		t.Fatal(err)
	}
	if m["t9"] != "web01" {
		t.Errorf("hostname map wrong: %+v", m)
	}
}

func TestHTTPClientAcknowledgeSendsAction(t *testing.T) {
	var captured rpcReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &captured)
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"eventids":["55"]}}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	if err := c.Acknowledge(context.Background(), []string{"55"}, ZbxActionAcknowledge|ZbxActionAddMessage, "fixing"); err != nil {
		t.Fatal(err)
	}
	if captured.Method != "event.acknowledge" {
		t.Errorf("method: %q", captured.Method)
	}
	if captured.Params["message"] != "fixing" {
		t.Errorf("message param missing: %+v", captured.Params)
	}
}

func TestHTTPClientNon2xx(t *testing.T) {
	// A gateway returning valid JSON (no result/error) with a non-2xx status
	// must surface as an error, not a false-healthy empty result.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"message":"gateway down"}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	if _, err := c.Problems(context.Background()); err == nil {
		t.Fatal("want error on HTTP 502 response, got nil")
	}
}

func TestHTTPClientRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":"boom"}}`)
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "tok", 5*time.Second)
	_, err := c.Problems(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want rpc error containing data, got %v", err)
	}
}
