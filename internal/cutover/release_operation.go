package cutover

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrBuildNotPassed = errors.New("build has not passed")
	ErrNotApproved    = errors.New("release is not approved")
	ErrNoReceipt      = errors.New("no cutover receipt exists")
)

type Request struct {
	Domain         string `json:"domain"`
	Name           string `json:"name"`
	PreviousTarget string `json:"previous_target"`
	NewTarget      string `json:"new_target"`
	BuildID        string `json:"build_id"`
	BuildPassed    bool   `json:"build_passed"`
	Approved       bool   `json:"approved"`
	TTL            int    `json:"ttl"`
}

type RecordChange struct {
	Domain  string `json:"domain"`
	Name    string `json:"name"`
	Target  string `json:"target"`
	TTL     int    `json:"ttl"`
	BuildID string `json:"build_id"`
	Action  string `json:"action"`
}

type Receipt struct {
	Domain         string    `json:"domain"`
	Name           string    `json:"name"`
	PreviousTarget string    `json:"previous_target"`
	CurrentTarget  string    `json:"current_target"`
	TTL            int       `json:"ttl"`
	BuildID        string    `json:"build_id"`
	ChangedAt      time.Time `json:"changed_at"`
}

type Store struct {
	mu       sync.RWMutex
	receipts map[string]Receipt
}

func NewStore() *Store {
	return &Store{receipts: make(map[string]Receipt)}
}

func PlanCutover(req Request) (RecordChange, error) {
	if !req.BuildPassed {
		return RecordChange{}, ErrBuildNotPassed
	}
	if !req.Approved {
		return RecordChange{}, ErrNotApproved
	}
	if req.Domain == "" || req.Name == "" || req.PreviousTarget == "" || req.NewTarget == "" || req.BuildID == "" {
		return RecordChange{}, errors.New("domain, name, targets, and build_id are required")
	}
	if req.TTL < 30 || req.TTL > 300 {
		return RecordChange{}, errors.New("ttl must be between 30 and 300 seconds")
	}
	return RecordChange{Domain: req.Domain, Name: req.Name, Target: req.NewTarget, TTL: req.TTL, BuildID: req.BuildID, Action: "cutover"}, nil
}

func (s *Store) Record(req Request, previousTarget string, changedAt time.Time) Receipt {
	r := Receipt{Domain: req.Domain, Name: req.Name, PreviousTarget: previousTarget, CurrentTarget: req.NewTarget, TTL: req.TTL, BuildID: req.BuildID, ChangedAt: changedAt}
	s.mu.Lock()
	s.receipts[key(req.Domain, req.Name)] = r
	s.mu.Unlock()
	return r
}

func (s *Store) PlanRollback(domain, name string) (RecordChange, error) {
	s.mu.RLock()
	r, ok := s.receipts[key(domain, name)]
	s.mu.RUnlock()
	if !ok {
		return RecordChange{}, ErrNoReceipt
	}
	return RecordChange{Domain: domain, Name: name, Target: r.PreviousTarget, TTL: r.TTL, BuildID: r.BuildID, Action: "rollback"}, nil
}

func (s *Store) Get(domain, name string) (Receipt, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.receipts[key(domain, name)]
	return r, ok
}

func key(domain, name string) string { return fmt.Sprintf("%s/%s", domain, name) }
