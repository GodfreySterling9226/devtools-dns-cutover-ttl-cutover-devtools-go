package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"example.com/devtools-dns-cutover/internal/cutover"
	"example.com/devtools-dns-cutover/internal/infrai"
)

type dnsWriter interface {
	ZoneID(context.Context, string) (string, error)
	CurrentCNAME(context.Context, string, string) (string, error)
	UpsertCNAME(context.Context, string, string, string, int, string) error
}

type server struct {
	dns   dnsWriter
	store *cutover.Store
	now   func() time.Time
}

var errPreviousTargetMismatch = errors.New("previous_target does not match live CNAME")

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	s := &server{dns: infrai.NewClient(apiKey), store: cutover.NewStore(), now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cutovers", s.cutover)
	mux.HandleFunc("POST /rollbacks", s.rollback)
	mux.HandleFunc("GET /diagnostics", s.diagnostics)
	log.Printf("cutoverd listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *server) cutover(w http.ResponseWriter, r *http.Request) {
	var input cutover.Request
	if err := decode(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	change, err := cutover.PlanCutover(input)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	previousTarget, err := s.applyCutover(r.Context(), change, input.PreviousTarget)
	if err != nil {
		if errors.Is(err, errPreviousTargetMismatch) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeUpstreamError(w, err)
		return
	}
	receipt := s.store.Record(input, previousTarget, s.now().UTC())
	writeJSON(w, http.StatusCreated, receipt)
}

type rollbackRequest struct {
	Domain string `json:"domain"`
	Name   string `json:"name"`
}

func (s *server) rollback(w http.ResponseWriter, r *http.Request) {
	var input rollbackRequest
	if err := decode(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	change, err := s.store.PlanRollback(input.Domain, input.Name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := s.apply(r.Context(), change); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, change)
}

func (s *server) diagnostics(w http.ResponseWriter, r *http.Request) {
	domain, name := r.URL.Query().Get("domain"), r.URL.Query().Get("name")
	receipt, ok := s.store.Get(domain, name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"state": "no_cutover_recorded"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "cutover_recorded", "receipt": receipt, "rollback_target": receipt.PreviousTarget})
}

func (s *server) apply(ctx context.Context, change cutover.RecordChange) error {
	zoneID, err := s.dns.ZoneID(ctx, change.Domain)
	if err != nil {
		return err
	}
	keyInput := change.Action + ":" + change.Domain + ":" + change.Name + ":" + change.Target + ":" + change.BuildID
	sum := sha256.Sum256([]byte(keyInput))
	return s.dns.UpsertCNAME(ctx, zoneID, change.Name, change.Target, change.TTL, hex.EncodeToString(sum[:]))
}

func (s *server) applyCutover(ctx context.Context, change cutover.RecordChange, claimedTarget string) (string, error) {
	zoneID, err := s.dns.ZoneID(ctx, change.Domain)
	if err != nil {
		return "", err
	}
	currentTarget, err := s.dns.CurrentCNAME(ctx, change.Domain, change.Name)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(strings.TrimSuffix(claimedTarget, "."), strings.TrimSuffix(currentTarget, ".")) {
		return "", errPreviousTargetMismatch
	}
	keyInput := change.Action + ":" + change.Domain + ":" + change.Name + ":" + change.Target + ":" + change.BuildID
	sum := sha256.Sum256([]byte(keyInput))
	if err := s.dns.UpsertCNAME(ctx, zoneID, change.Name, change.Target, change.TTL, hex.EncodeToString(sum[:])); err != nil {
		return "", err
	}
	return strings.TrimSuffix(currentTarget, "."), nil
}

func decode(r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	var apiErr *infrai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Code, "message": apiErr.Message})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "dns_operation_failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
