package daemonapi

import (
	"errors"
	"net/http"
	"testing"

	"filesyncengine/internal/config"
)

func TestPrepareHTTPServerSkipsDisabledListener(t *testing.T) {
	called := false
	server, plan, err := PrepareHTTPServer(PrepareHTTPServerOptions{
		Config:     &config.Config{},
		ConfigPath: "/tmp/config.jsonc",
		Handler:    http.NewServeMux(),
		EnsureTLSAssets: func(*config.Config, string) error {
			called = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("PrepareHTTPServer returned error: %v", err)
	}
	if server != nil {
		t.Fatalf("expected no HTTP server when api.listen is empty")
	}
	if plan.Enabled {
		t.Fatalf("expected disabled plan for empty api.listen")
	}
	if called {
		t.Fatalf("TLS asset bootstrap should not run when API listener is disabled")
	}
}

func TestPrepareHTTPServerBootstrapsTLSForNonLoopbackAutoListener(t *testing.T) {
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:9000"}}
	calledWithPath := ""
	server, plan, err := PrepareHTTPServer(PrepareHTTPServerOptions{
		Config:     &cfg,
		ConfigPath: "/tmp/fse/config.jsonc",
		Handler:    http.NewServeMux(),
		EnsureTLSAssets: func(c *config.Config, path string) error {
			calledWithPath = path
			c.API.Encryption.CertFile = "/tmp/fse/api.crt"
			c.API.Encryption.KeyFile = "/tmp/fse/api.key"
			return nil
		},
	})
	if err != nil {
		t.Fatalf("PrepareHTTPServer returned error: %v", err)
	}
	if calledWithPath != "/tmp/fse/config.jsonc" {
		t.Fatalf("expected TLS assets to be bootstrapped for non-loopback auto listener, got path %q", calledWithPath)
	}
	if server == nil || server.Addr != "0.0.0.0:9000" {
		t.Fatalf("expected server for configured listener, got %#v", server)
	}
	if !plan.Enabled || !plan.RequiresTLS {
		t.Fatalf("expected enabled TLS plan, got %#v", plan)
	}
	if plan.CertFile != "/tmp/fse/api.crt" || plan.KeyFile != "/tmp/fse/api.key" {
		t.Fatalf("expected plan to use bootstrapped cert/key, got %#v", plan)
	}
}

func TestPrepareHTTPServerPropagatesTLSBootstrapFailure(t *testing.T) {
	want := errors.New("cannot write cert")
	cfg := config.Config{API: config.APIConfig{Listen: "0.0.0.0:9000"}}
	server, plan, err := PrepareHTTPServer(PrepareHTTPServerOptions{
		Config:     &cfg,
		ConfigPath: "/tmp/fse/config.jsonc",
		Handler:    http.NewServeMux(),
		EnsureTLSAssets: func(*config.Config, string) error {
			return want
		},
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected TLS bootstrap error %v, got %v", want, err)
	}
	if server != nil || plan.Enabled {
		t.Fatalf("expected no server/plan on TLS bootstrap failure, got server=%#v plan=%#v", server, plan)
	}
}
