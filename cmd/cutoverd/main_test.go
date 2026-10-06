package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/devtools-dns-cutover/internal/cutover"
)

type fakeDNS struct {
	current string
	upserts int
}

func (d *fakeDNS) ZoneID(context.Context, string) (string, error) { return "zone-1", nil }
func (d *fakeDNS) CurrentCNAME(context.Context, string, string) (string, error) {
	return d.current, nil
}
func (d *fakeDNS) UpsertCNAME(context.Context, string, string, string, int, string) error {
	d.upserts++
	return nil
}

func TestCutoverVerifiesAndRecordsLiveTarget(t *testing.T) {
	dns := &fakeDNS{current: "tools-old.example.net"}
	s := &server{dns: dns, store: cutover.NewStore(), now: func() time.Time { return time.Unix(1, 0) }}
	body := `{"domain":"example.com","name":"tools","previous_target":"TOOLS-OLD.EXAMPLE.NET.","new_target":"tools-new.example.net","build_id":"build-1842","build_passed":true,"approved":true,"ttl":60}`
	w := httptest.NewRecorder()
	s.cutover(w, httptest.NewRequest(http.MethodPost, "/cutovers", strings.NewReader(body)))

	if w.Code != http.StatusCreated || dns.upserts != 1 {
		t.Fatalf("status = %d, upserts = %d, body = %s", w.Code, dns.upserts, w.Body.String())
	}
	var receipt cutover.Receipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.PreviousTarget != dns.current {
		t.Fatalf("previous target = %q, want verified %q", receipt.PreviousTarget, dns.current)
	}
}

func TestCutoverRejectsUnverifiedPreviousTarget(t *testing.T) {
	dns := &fakeDNS{current: "actual.example.net"}
	s := &server{dns: dns, store: cutover.NewStore(), now: time.Now}
	body := `{"domain":"example.com","name":"tools","previous_target":"attacker.example.net","new_target":"tools-new.example.net","build_id":"build-1842","build_passed":true,"approved":true,"ttl":60}`
	w := httptest.NewRecorder()
	s.cutover(w, httptest.NewRequest(http.MethodPost, "/cutovers", strings.NewReader(body)))

	if w.Code != http.StatusConflict || dns.upserts != 0 {
		t.Fatalf("status = %d, upserts = %d, body = %s", w.Code, dns.upserts, w.Body.String())
	}
	if _, ok := s.store.Get("example.com", "tools"); ok {
		t.Fatal("receipt recorded after previous_target mismatch")
	}
}
