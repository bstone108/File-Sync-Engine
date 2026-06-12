package apicontrol

import (
	"strings"
	"testing"

	"filesyncengine/internal/api"
)

func TestHandleServiceCommandRendersReviewableHandoff(t *testing.T) {
	resp, err := HandleServiceCommand(api.ServiceCommandRequest{Action: "restart", Platform: "systemd", ServiceName: "fse"})
	if err != nil {
		t.Fatalf("service command response: %v", err)
	}
	if resp.Action != "restart" || resp.Platform != "systemd" || resp.ServiceName != "fse" || resp.Status != "accepted" {
		t.Fatalf("unexpected service command response: %+v", resp)
	}
	for _, want := range []string{"systemctl status fse", "systemctl restart fse", "Review before running"} {
		if !strings.Contains(resp.Handoff, want) {
			t.Fatalf("service handoff missing %q:\n%s", want, resp.Handoff)
		}
	}
}

func TestHandleServiceCommandRejectsInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  api.ServiceCommandRequest
	}{
		{name: "unsupported action", req: api.ServiceCommandRequest{Action: "install", Platform: "systemd", ServiceName: "fse"}},
		{name: "missing platform", req: api.ServiceCommandRequest{Action: "status", ServiceName: "fse"}},
		{name: "missing service name", req: api.ServiceCommandRequest{Action: "status", Platform: "systemd"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := HandleServiceCommand(tc.req); err == nil {
				t.Fatal("expected service command request to fail")
			}
		})
	}
}
