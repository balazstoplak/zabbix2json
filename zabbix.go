package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Client is the Zabbix API surface zabbix2json needs (mockable in tests).
type Client interface {
	Problems(ctx context.Context) ([]Problem, error)
	Hostnames(ctx context.Context, triggerIDs []string) (map[string]string, error)
	Acknowledge(ctx context.Context, eventIDs []string, action int, message string) error
}

// HTTPClient talks to a real Zabbix 7.0 JSON-RPC endpoint with a Bearer token.
type HTTPClient struct {
	url   string
	token string
	hc    *http.Client
}

func NewHTTPClient(url, token string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{url: url, token: token, hc: &http.Client{Timeout: timeout}}
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *HTTPClient) call(ctx context.Context, method string, params interface{}, result interface{}) error {
	reqBody, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var rpc rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if rpc.Error != nil {
		return fmt.Errorf("zabbix %s error %d: %s (%s)", method, rpc.Error.Code, rpc.Error.Message, rpc.Error.Data)
	}
	if result != nil {
		if err := json.Unmarshal(rpc.Result, result); err != nil {
			return fmt.Errorf("unmarshal %s result: %w", method, err)
		}
	}
	return nil
}

type rawProblem struct {
	EventID      string `json:"eventid"`
	ObjectID     string `json:"objectid"`
	Name         string `json:"name"`
	Severity     string `json:"severity"`
	Clock        string `json:"clock"`
	Acknowledged string `json:"acknowledged"`
	Suppressed   string `json:"suppressed"`
	Opdata       string `json:"opdata"`
}

func atoiSafe(s string) int { n, _ := strconv.Atoi(s); return n }
func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func (c *HTTPClient) Problems(ctx context.Context) ([]Problem, error) {
	params := map[string]interface{}{
		"output":    []string{"eventid", "objectid", "name", "severity", "clock", "r_clock", "acknowledged", "suppressed", "opdata"},
		"recent":    false,
		"sortfield": []string{"eventid"},
		"sortorder": "DESC",
	}
	var raw []rawProblem
	if err := c.call(ctx, "problem.get", params, &raw); err != nil {
		return nil, err
	}
	out := make([]Problem, 0, len(raw))
	for _, r := range raw {
		out = append(out, Problem{
			EventID:      r.EventID,
			TriggerID:    r.ObjectID,
			Name:         r.Name,
			Severity:     atoiSafe(r.Severity),
			Clock:        atoi64(r.Clock),
			Acknowledged: r.Acknowledged == "1",
			Suppressed:   r.Suppressed == "1",
			Opdata:       r.Opdata,
		})
	}
	return out, nil
}

type rawTrigger struct {
	TriggerID string `json:"triggerid"`
	Hosts     []struct {
		Name string `json:"name"`
	} `json:"hosts"`
}

func (c *HTTPClient) Hostnames(ctx context.Context, triggerIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(triggerIDs))
	if len(triggerIDs) == 0 {
		return out, nil
	}
	params := map[string]interface{}{
		"output":      []string{"triggerid"},
		"triggerids":  triggerIDs,
		"selectHosts": []string{"name"},
	}
	var raw []rawTrigger
	if err := c.call(ctx, "trigger.get", params, &raw); err != nil {
		return nil, err
	}
	for _, t := range raw {
		if len(t.Hosts) > 0 {
			out[t.TriggerID] = t.Hosts[0].Name
		}
	}
	return out, nil
}

func (c *HTTPClient) Acknowledge(ctx context.Context, eventIDs []string, action int, message string) error {
	params := map[string]interface{}{
		"eventids": eventIDs,
		"action":   action,
	}
	if action&ZbxActionAddMessage != 0 && message != "" {
		params["message"] = message
	}
	return c.call(ctx, "event.acknowledge", params, nil)
}
