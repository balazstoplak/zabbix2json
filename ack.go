package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// ackActionForCmd maps a nagios cmd_typ to a Zabbix event.acknowledge action.
func ackActionForCmd(cmdTyp int, hasMessage bool) (int, bool) {
	switch cmdTyp {
	case 33, 34: // ACKNOWLEDGE_HOST_PROBLEM / ACKNOWLEDGE_SVC_PROBLEM
		action := ZbxActionAcknowledge
		if hasMessage {
			action |= ZbxActionAddMessage
		}
		return action, true
	case 51, 52: // REMOVE_HOST_ACKNOWLEDGEMENT / REMOVE_SVC_ACKNOWLEDGEMENT
		return ZbxActionUnack, true
	default:
		return 0, false
	}
}

type ackResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func writeAck(w http.ResponseWriter, code int, resp ackResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeAck(w, http.StatusBadRequest, ackResponse{false, "bad form"})
		return
	}
	cmdTyp, _ := strconv.Atoi(r.Form.Get("cmd_typ"))
	host := r.Form.Get("host")
	service := r.Form.Get("service")
	message := r.Form.Get("com_data")

	action, ok := ackActionForCmd(cmdTyp, message != "")
	if !ok {
		writeAck(w, http.StatusBadRequest, ackResponse{false, "unsupported cmd_typ"})
		return
	}

	problems, err := s.client.Problems(r.Context())
	if err != nil {
		writeAck(w, http.StatusBadGateway, ackResponse{false, "zabbix unavailable"})
		return
	}
	triggerIDs := make([]string, 0, len(problems))
	for _, p := range problems {
		triggerIDs = append(triggerIDs, p.TriggerID)
	}
	hosts, _ := s.client.Hostnames(r.Context(), triggerIDs)
	rows := BuildServices(problems, hosts, 0)

	var eventIDs []string
	for _, row := range rows {
		if row.Hostname != host {
			continue
		}
		// host-level commands (33/51) match any service on the host; svc commands match the name.
		if (cmdTyp == 34 || cmdTyp == 52) && row.Service != service {
			continue
		}
		eventIDs = append(eventIDs, row.EventID)
	}
	if len(eventIDs) == 0 {
		writeAck(w, http.StatusNotFound, ackResponse{false, "no matching active problem"})
		return
	}

	if err := s.client.Acknowledge(r.Context(), eventIDs, action, message); err != nil {
		writeAck(w, http.StatusBadGateway, ackResponse{false, err.Error()})
		return
	}
	writeAck(w, http.StatusOK, ackResponse{Success: true})
}
