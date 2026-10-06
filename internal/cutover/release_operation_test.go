package cutover

import (
	"errors"
	"testing"
	"time"
)

func TestPlanCutover(t *testing.T) {
	base := Request{Domain: "example.com", Name: "tools", PreviousTarget: "tools-old.example.net", NewTarget: "tools-new.example.net", BuildID: "build-1842", BuildPassed: true, Approved: true, TTL: 60}
	tests := []struct {
		name    string
		change  func(*Request)
		wantErr error
	}{
		{name: "passed and approved"},
		{name: "failed build", change: func(r *Request) { r.BuildPassed = false }, wantErr: ErrBuildNotPassed},
		{name: "approval absent", change: func(r *Request) { r.Approved = false }, wantErr: ErrNotApproved},
		{name: "ttl too long", change: func(r *Request) { r.TTL = 600 }, wantErr: errors.New("ttl must be between 30 and 300 seconds")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			if tt.change != nil {
				tt.change(&req)
			}
			got, err := PlanCutover(req)
			if tt.wantErr != nil {
				if err == nil || err.Error() != tt.wantErr.Error() {
					t.Fatalf("PlanCutover() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanCutover() error = %v", err)
			}
			if got.Target != base.NewTarget || got.Action != "cutover" || got.TTL != 60 {
				t.Fatalf("PlanCutover() = %+v", got)
			}
		})
	}
}

func TestRollbackUsesRecordedPreviousTarget(t *testing.T) {
	store := NewStore()
	req := Request{Domain: "example.com", Name: "tools", PreviousTarget: "tools-old.example.net", NewTarget: "tools-new.example.net", BuildID: "build-1842", TTL: 60}
	store.Record(req, req.PreviousTarget, time.Unix(1700000000, 0))

	got, err := store.PlanRollback(req.Domain, req.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Target != req.PreviousTarget || got.Action != "rollback" {
		t.Fatalf("PlanRollback() = %+v", got)
	}
}
