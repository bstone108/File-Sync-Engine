package daemonapi

import (
	"net/http"

	"filesyncengine/internal/config"
)

// EnsureTLSAssets mutates cfg with generated certificate paths when TLS is required.
type EnsureTLSAssets func(cfg *config.Config, configPath string) error

type PrepareHTTPServerOptions struct {
	Config          *config.Config
	ConfigPath      string
	Handler         http.Handler
	EnsureTLSAssets EnsureTLSAssets
}

type HTTPServerPlan struct {
	Enabled     bool
	RequiresTLS bool
	Listen      string
	CertFile    string
	KeyFile     string
}

type HTTPServerStarter interface {
	ListenAndServe() error
	ListenAndServeTLS(certFile, keyFile string) error
}

func ServePreparedHTTPServer(server HTTPServerStarter, plan HTTPServerPlan, logListening func(string), logStopped func(error)) {
	if server == nil || !plan.Enabled {
		return
	}
	if logListening != nil {
		logListening(plan.Listen)
	}
	var err error
	if plan.RequiresTLS {
		err = server.ListenAndServeTLS(plan.CertFile, plan.KeyFile)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed && logStopped != nil {
		logStopped(err)
	}
}

func PrepareHTTPServer(opts PrepareHTTPServerOptions) (*http.Server, HTTPServerPlan, error) {
	if opts.Config == nil || opts.Config.API.Listen == "" {
		return nil, HTTPServerPlan{}, nil
	}
	if opts.Config.API.RequiresTLS() && opts.EnsureTLSAssets != nil {
		if err := opts.EnsureTLSAssets(opts.Config, opts.ConfigPath); err != nil {
			return nil, HTTPServerPlan{}, err
		}
	}
	plan := HTTPServerPlan{
		Enabled:     true,
		RequiresTLS: opts.Config.API.RequiresTLS(),
		Listen:      opts.Config.API.Listen,
		CertFile:    opts.Config.API.Encryption.CertFile,
		KeyFile:     opts.Config.API.Encryption.KeyFile,
	}
	return &http.Server{Addr: opts.Config.API.Listen, Handler: opts.Handler}, plan, nil
}
