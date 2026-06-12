package daemonapihandlers

import (
	"context"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/pairing"
)

type recordingStaticRegistrar struct {
	peerCommand      api.PeerCommandHandler
	folderCommand    api.FolderCommandHandler
	discoveryCommand api.DiscoveryCommandHandler
	configRead       api.ConfigReadHandler
	apiTrust         api.APITrustHandler
	apiTrustCommand  api.APITrustCommandHandler
	identityPackage  api.IdentityPackageHandler
	identityImport   api.IdentityImportHandler
	filesystemBrowse api.FilesystemBrowseHandler
	configUpdate     api.ConfigUpdateHandler
	serviceCommand   api.ServiceCommandHandler
	transferCommand  api.TransferCommandHandler
	webGUICommand    api.WebGUICommandHandler
}

func (r *recordingStaticRegistrar) SetPeerCommandHandler(handler api.PeerCommandHandler) {
	r.peerCommand = handler
}
func (r *recordingStaticRegistrar) SetFolderCommandHandler(handler api.FolderCommandHandler) {
	r.folderCommand = handler
}
func (r *recordingStaticRegistrar) SetDiscoveryCommandHandler(handler api.DiscoveryCommandHandler) {
	r.discoveryCommand = handler
}
func (r *recordingStaticRegistrar) SetConfigReadHandler(handler api.ConfigReadHandler) {
	r.configRead = handler
}
func (r *recordingStaticRegistrar) SetAPITrustHandler(handler api.APITrustHandler) {
	r.apiTrust = handler
}
func (r *recordingStaticRegistrar) SetAPITrustCommandHandler(handler api.APITrustCommandHandler) {
	r.apiTrustCommand = handler
}
func (r *recordingStaticRegistrar) SetIdentityPackageHandler(handler api.IdentityPackageHandler) {
	r.identityPackage = handler
}
func (r *recordingStaticRegistrar) SetIdentityImportHandler(handler api.IdentityImportHandler) {
	r.identityImport = handler
}
func (r *recordingStaticRegistrar) SetFilesystemBrowseHandler(handler api.FilesystemBrowseHandler) {
	r.filesystemBrowse = handler
}
func (r *recordingStaticRegistrar) SetConfigUpdateHandler(handler api.ConfigUpdateHandler) {
	r.configUpdate = handler
}
func (r *recordingStaticRegistrar) SetServiceCommandHandler(handler api.ServiceCommandHandler) {
	r.serviceCommand = handler
}
func (r *recordingStaticRegistrar) SetTransferCommandHandler(handler api.TransferCommandHandler) {
	r.transferCommand = handler
}
func (r *recordingStaticRegistrar) SetWebGUICommandHandler(handler api.WebGUICommandHandler) {
	r.webGUICommand = handler
}

func TestRegisterStaticHandlersInstallsStartupCommandHandlers(t *testing.T) {
	registrar := &recordingStaticRegistrar{}
	called := map[string]bool{}

	RegisterStatic(registrar, StaticHandlers{
		PeerCommand: func(context.Context, api.PeerCommandRequest) (api.PeerCommandResponse, error) {
			called["peer"] = true
			return api.PeerCommandResponse{}, nil
		},
		FolderCommand: func(context.Context, api.FolderCommandRequest) (api.FolderCommandResponse, error) {
			called["folder"] = true
			return api.FolderCommandResponse{}, nil
		},
		DiscoveryCommand: func(context.Context, api.DiscoveryCommandRequest) (api.DiscoveryCommandResponse, error) {
			called["discovery"] = true
			return api.DiscoveryCommandResponse{}, nil
		},
		ConfigRead: func(context.Context) (config.Config, error) {
			called["configRead"] = true
			return config.Config{}, nil
		},
		APITrust: func(context.Context) (api.APITrustResponse, error) {
			called["apiTrust"] = true
			return api.APITrustResponse{}, nil
		},
		APITrustCommand: func(context.Context, api.APITrustCommandRequest) (api.APITrustCommandResponse, error) {
			called["apiTrustCommand"] = true
			return api.APITrustCommandResponse{}, nil
		},
		IdentityPackage: func(context.Context, api.IdentityPackageRequest) (pairing.IdentityPackage, error) {
			called["identityPackage"] = true
			return pairing.IdentityPackage{}, nil
		},
		IdentityImport: func(context.Context, api.IdentityImportRequest) (api.IdentityImportResponse, error) {
			called["identityImport"] = true
			return api.IdentityImportResponse{}, nil
		},
		FilesystemBrowse: func(context.Context, api.FilesystemBrowseRequest) (api.FilesystemBrowseResponse, error) {
			called["filesystemBrowse"] = true
			return api.FilesystemBrowseResponse{}, nil
		},
		ConfigUpdate: func(context.Context, api.ConfigUpdateRequest) (api.ConfigUpdateResponse, error) {
			called["configUpdate"] = true
			return api.ConfigUpdateResponse{}, nil
		},
		ServiceCommand: func(context.Context, api.ServiceCommandRequest) (api.ServiceCommandResponse, error) {
			called["service"] = true
			return api.ServiceCommandResponse{}, nil
		},
		TransferCommand: func(context.Context, api.TransferCommandRequest) (api.TransferCommandResponse, error) {
			called["transfer"] = true
			return api.TransferCommandResponse{}, nil
		},
		WebGUICommand: func(context.Context, api.WebGUICommandRequest) (api.WebGUICommandResponse, error) {
			called["webGUI"] = true
			return api.WebGUICommandResponse{}, nil
		},
	})

	invokeRegisteredStaticHandlers(t, registrar)
	for _, name := range []string{"peer", "folder", "discovery", "configRead", "apiTrust", "apiTrustCommand", "identityPackage", "identityImport", "filesystemBrowse", "configUpdate", "service", "transfer", "webGUI"} {
		if !called[name] {
			t.Fatalf("handler %s was not registered/invoked", name)
		}
	}
}

func invokeRegisteredStaticHandlers(t *testing.T, registrar *recordingStaticRegistrar) {
	t.Helper()
	ctx := context.Background()
	calls := []struct {
		name string
		fn   func() error
	}{
		{"peer", func() error { _, err := registrar.peerCommand(ctx, api.PeerCommandRequest{}); return err }},
		{"folder", func() error { _, err := registrar.folderCommand(ctx, api.FolderCommandRequest{}); return err }},
		{"discovery", func() error { _, err := registrar.discoveryCommand(ctx, api.DiscoveryCommandRequest{}); return err }},
		{"configRead", func() error { _, err := registrar.configRead(ctx); return err }},
		{"apiTrust", func() error { _, err := registrar.apiTrust(ctx); return err }},
		{"apiTrustCommand", func() error { _, err := registrar.apiTrustCommand(ctx, api.APITrustCommandRequest{}); return err }},
		{"identityPackage", func() error { _, err := registrar.identityPackage(ctx, api.IdentityPackageRequest{}); return err }},
		{"identityImport", func() error { _, err := registrar.identityImport(ctx, api.IdentityImportRequest{}); return err }},
		{"filesystemBrowse", func() error { _, err := registrar.filesystemBrowse(ctx, api.FilesystemBrowseRequest{}); return err }},
		{"configUpdate", func() error { _, err := registrar.configUpdate(ctx, api.ConfigUpdateRequest{}); return err }},
		{"service", func() error { _, err := registrar.serviceCommand(ctx, api.ServiceCommandRequest{}); return err }},
		{"transfer", func() error { _, err := registrar.transferCommand(ctx, api.TransferCommandRequest{}); return err }},
		{"webGUI", func() error { _, err := registrar.webGUICommand(ctx, api.WebGUICommandRequest{}); return err }},
	}
	for _, call := range calls {
		if call.fn == nil {
			t.Fatalf("handler %s was not installed", call.name)
		}
		if err := call.fn(); err != nil {
			t.Fatalf("handler %s returned error: %v", call.name, err)
		}
	}
}
