package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAckActionForCmd(t *testing.T) {
	if a, ok := ackActionForCmd(34, true); !ok || a != (ZbxActionAcknowledge|ZbxActionAddMessage) {
		t.Errorf("svc ack w/ message wrong: %d %v", a, ok)
	}
	if a, ok := ackActionForCmd(33, false); !ok || a != ZbxActionAcknowledge {
		t.Errorf("host ack wrong: %d %v", a, ok)
	}
	if a, ok := ackActionForCmd(52, false); !ok || a != ZbxActionUnack {
		t.Errorf("remove ack wrong: %d %v", a, ok)
	}
	if _, ok := ackActionForCmd(99, false); ok {
		t.Error("unknown cmd_typ should not be ok")
	}
}

func TestHandleCmdResolvesAndAcks(t *testing.T) {
	c := &fakeClient{
		problems: []Problem{
			{EventID: "55", TriggerID: "t1", Name: "PING", Severity: 5, Clock: 100},
			{EventID: "56", TriggerID: "t2", Name: "DISK", Severity: 5, Clock: 100},
		},
		hosts: map[string]string{"t1": "web01", "t2": "web01"},
	}
	form := "cmd_typ=34&host=web01&service=PING&com_data=fixing&com_author=me"
	req := httptest.NewRequest(http.MethodPost, "/cmd.cgi", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	testServer(c).handleCmd(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Errorf("want success: %s", rec.Body.String())
	}
	if len(c.ackCalls) != 1 {
		t.Fatalf("want 1 ack call, got %d", len(c.ackCalls))
	}
	call := c.ackCalls[0]
	if len(call.eventIDs) != 1 || call.eventIDs[0] != "55" {
		t.Errorf("wrong eventids: %+v", call.eventIDs)
	}
	if call.action != (ZbxActionAcknowledge|ZbxActionAddMessage) || call.message != "fixing" {
		t.Errorf("wrong action/message: %+v", call)
	}
}

func TestHandleCmdNoMatchFails(t *testing.T) {
	c := &fakeClient{problems: []Problem{}, hosts: map[string]string{}}
	form := "cmd_typ=34&host=nope&service=PING&com_data=x"
	req := httptest.NewRequest(http.MethodPost, "/cmd.cgi", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	testServer(c).handleCmd(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"success":false`) {
		t.Errorf("want failure body: %s", rec.Body.String())
	}
	if len(c.ackCalls) != 0 {
		t.Errorf("should not have called Acknowledge")
	}
}

func TestHandleCmdBadCmdTyp(t *testing.T) {
	c := &fakeClient{}
	form := "cmd_typ=99&host=h&service=s"
	req := httptest.NewRequest(http.MethodPost, "/cmd.cgi", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	testServer(c).handleCmd(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}
