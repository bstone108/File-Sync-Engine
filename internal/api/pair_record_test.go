package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"filesyncengine/internal/link"
)

func TestPairCodeRecordServesPayloadWithoutAPIKey(t *testing.T) {
	server := NewServer(State{}, "secret")
	namespace := link.CodeNamespacePrefix + "abc"
	server.SetPairCodeRecordHandler(func(context.Context, string) ([]byte, error) {
		return []byte("sealed"), nil
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/pair-code/record?namespace="+url.QueryEscape(namespace), nil)
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "sealed" {
		t.Fatalf("record code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}

	missing := httptest.NewRecorder()
	server.Router().ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/v1/pair-code/record?namespace=other", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("foreign namespace code = %d", missing.Code)
	}

	authed := httptest.NewRequest(http.MethodGet, "/v1/pair-code", nil)
	authed.Header.Set("X-FSE-API-Key", "secret")
	denied := httptest.NewRecorder()
	server.Router().ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/v1/pair-code", nil))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("pair-code without a key = %d", denied.Code)
	}
}
