package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"filesyncengine/internal/link"
	"filesyncengine/internal/peeridentity"
)

type dropResponseWriter struct {
	header http.Header
	err    error
}

func (d *dropResponseWriter) Header() http.Header {
	if d.header == nil {
		d.header = http.Header{}
	}
	return d.header
}

func (d *dropResponseWriter) WriteHeader(int) {}

func (d *dropResponseWriter) Write([]byte) (int, error) {
	return 0, d.err
}

func TestDroppedClaimLetsSameRedeemerRetry(t *testing.T) {
	publisher, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	redeemer, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	other, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	service := link.NewService(link.NewMemoryDirectory())
	code, _, err := service.Issue(context.Background(), publisher, link.Profile{
		DiscoveryID: publisher.PublicKey,
		NodeName:    "publisher",
		Addresses:   []link.Address{{Kind: "manual", Address: "http://10.0.0.2:22420"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(State{}, "")
	server.SetPairCodeClaimHandler(func(_ context.Context, req link.ClaimRequest) (link.ClaimResponse, error) {
		record, claimErr := service.Claim(context.Background(), req)
		if claimErr != nil {
			return link.ClaimResponse{}, claimErr
		}
		return link.ClaimResponse{
			DiscoveryID: publisher.PublicKey,
			NodeName:    "publisher",
			APIKey:      "publisher-api",
			Profile:     record.Profile(),
		}, nil
	})
	server.SetPairCodeClaimCommit(func(req link.ClaimRequest) error {
		return service.CommitClaim(req.Code, req.DiscoveryID)
	})

	claimBody := func(identity peeridentity.Identity, name, apiKey string) []byte {
		t.Helper()
		claim, signErr := link.SignClaim(identity, link.ClaimRequest{
			Code:     code,
			NodeName: name,
			APIKey:   apiKey,
			Profile:  link.Profile{DiscoveryID: identity.PublicKey, NodeName: name},
		})
		if signErr != nil {
			t.Fatal(signErr)
		}
		body, marshalErr := json.Marshal(claim)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return body
	}
	post := func(body []byte, w http.ResponseWriter) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/pair-code/claim", bytes.NewReader(body))
		server.handlePairCodeClaim(w, req)
	}

	same := claimBody(redeemer, "redeemer", "redeemer-api")
	post(same, &dropResponseWriter{err: errors.New("connection dropped")})

	if _, err := service.Lookup(context.Background(), code); err != nil {
		t.Fatalf("dropped claim unpublished the code: %v", err)
	}

	otherRec := httptest.NewRecorder()
	post(claimBody(other, "other", "other-api"), otherRec)
	if otherRec.Code != http.StatusConflict || !strings.Contains(otherRec.Body.String(), "reserved") {
		t.Fatalf("other redeemer during claim: %d %s", otherRec.Code, otherRec.Body.String())
	}

	retry := httptest.NewRecorder()
	post(same, retry)
	if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), "publisher-api") {
		t.Fatalf("retry: %d %s", retry.Code, retry.Body.String())
	}

	again := httptest.NewRecorder()
	post(same, again)
	if again.Code != http.StatusGone || !strings.Contains(again.Body.String(), "already used") {
		t.Fatalf("reuse after delivery: %d %s", again.Code, again.Body.String())
	}
	otherAfter := httptest.NewRecorder()
	post(claimBody(other, "other", "other-api"), otherAfter)
	if otherAfter.Code != http.StatusGone || !strings.Contains(otherAfter.Body.String(), "already used") {
		t.Fatalf("other redeemer after delivery: %d %s", otherAfter.Code, otherAfter.Body.String())
	}
}
