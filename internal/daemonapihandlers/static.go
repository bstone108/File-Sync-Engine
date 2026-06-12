package daemonapihandlers

import (
	"context"

	"filesyncengine/internal/api"
	"filesyncengine/internal/apicontrol"
	"filesyncengine/internal/config"
	"filesyncengine/internal/pairing"
	"filesyncengine/internal/state"
	"filesyncengine/internal/transfercontrol"
	"filesyncengine/internal/webgui"
)

// StaticRegistrar is the subset of the daemon API server whose handlers are
// installed at daemon startup and do not need to be refreshed for metadata-store
// hot reloads.
type StaticRegistrar interface {
	SetPeerCommandHandler(api.PeerCommandHandler)
	SetFolderCommandHandler(api.FolderCommandHandler)
	SetDiscoveryCommandHandler(api.DiscoveryCommandHandler)
	SetConfigReadHandler(api.ConfigReadHandler)
	SetAPITrustHandler(api.APITrustHandler)
	SetAPITrustCommandHandler(api.APITrustCommandHandler)
	SetIdentityPackageHandler(api.IdentityPackageHandler)
	SetIdentityImportHandler(api.IdentityImportHandler)
	SetFilesystemBrowseHandler(api.FilesystemBrowseHandler)
	SetConfigUpdateHandler(api.ConfigUpdateHandler)
	SetServiceCommandHandler(api.ServiceCommandHandler)
	SetTransferCommandHandler(api.TransferCommandHandler)
	SetWebGUICommandHandler(api.WebGUICommandHandler)
}

// StaticHandlers groups startup handlers that close over process-level runtime
// dependencies such as the active config path, transfer gate, and web GUI server.
type StaticHandlers struct {
	PeerCommand      api.PeerCommandHandler
	FolderCommand    api.FolderCommandHandler
	DiscoveryCommand api.DiscoveryCommandHandler
	ConfigRead       api.ConfigReadHandler
	APITrust         api.APITrustHandler
	APITrustCommand  api.APITrustCommandHandler
	IdentityPackage  api.IdentityPackageHandler
	IdentityImport   api.IdentityImportHandler
	FilesystemBrowse api.FilesystemBrowseHandler
	ConfigUpdate     api.ConfigUpdateHandler
	ServiceCommand   api.ServiceCommandHandler
	TransferCommand  api.TransferCommandHandler
	WebGUICommand    api.WebGUICommandHandler
}

// StaticRuntimeDeps are live daemon dependencies needed by startup API handlers.
// CurrentStore and CurrentConfig are functions so hot-reloaded runtime state is
// observed when each request executes instead of when handlers are registered.
type StaticRuntimeDeps struct {
	ConfigPath      string
	CurrentStore    func() state.JSONStore
	CurrentConfig   func() config.Config
	TransferControl *transfercontrol.Control
	WebGUIServer    *webgui.Server
}

// BuildStaticHandlers creates the startup handler group from live runtime
// dependencies, keeping process-boundary wiring out of cmd/fse.
func BuildStaticHandlers(deps StaticRuntimeDeps) StaticHandlers {
	return StaticHandlers{
		PeerCommand: func(ctx context.Context, req api.PeerCommandRequest) (api.PeerCommandResponse, error) {
			return apicontrol.HandlePeerCommand(deps.ConfigPath, req)
		},
		FolderCommand: func(ctx context.Context, req api.FolderCommandRequest) (api.FolderCommandResponse, error) {
			return apicontrol.HandleFolderCommand(deps.ConfigPath, req)
		},
		DiscoveryCommand: func(ctx context.Context, req api.DiscoveryCommandRequest) (api.DiscoveryCommandResponse, error) {
			return apicontrol.HandleDiscoveryCommand(deps.ConfigPath, req)
		},
		ConfigRead: func(ctx context.Context) (config.Config, error) {
			return config.LoadFile(deps.ConfigPath)
		},
		APITrust: func(ctx context.Context) (api.APITrustResponse, error) {
			return apicontrol.HandleAPITrustStatus(deps.ConfigPath)
		},
		APITrustCommand: func(ctx context.Context, req api.APITrustCommandRequest) (api.APITrustCommandResponse, error) {
			return apicontrol.HandleAPITrustCommand(deps.ConfigPath, req)
		},
		IdentityPackage: func(ctx context.Context, req api.IdentityPackageRequest) (pairing.IdentityPackage, error) {
			return apicontrol.HandleIdentityPackage(deps.ConfigPath, req)
		},
		IdentityImport: func(ctx context.Context, req api.IdentityImportRequest) (api.IdentityImportResponse, error) {
			return apicontrol.HandleIdentityImport(deps.ConfigPath, req)
		},
		FilesystemBrowse: api.BrowseLocalFilesystem,
		ConfigUpdate: func(ctx context.Context, req api.ConfigUpdateRequest) (api.ConfigUpdateResponse, error) {
			return apicontrol.HandleConfigUpdateWithStore(deps.ConfigPath, deps.CurrentStore(), req)
		},
		ServiceCommand: func(ctx context.Context, req api.ServiceCommandRequest) (api.ServiceCommandResponse, error) {
			return apicontrol.HandleServiceCommand(req)
		},
		TransferCommand: func(ctx context.Context, req api.TransferCommandRequest) (api.TransferCommandResponse, error) {
			return apicontrol.HandleTransferCommand(deps.TransferControl, req)
		},
		WebGUICommand: func(ctx context.Context, req api.WebGUICommandRequest) (api.WebGUICommandResponse, error) {
			return apicontrol.HandleWebGUICommandWithManager(deps.CurrentConfig(), req, deps.WebGUIServer, nil)
		},
	}
}

// RegisterStatic installs the complete startup handler group together so the
// daemon entrypoint can remain focused on process lifecycle wiring.
func RegisterStatic(registrar StaticRegistrar, handlers StaticHandlers) {
	registrar.SetPeerCommandHandler(handlers.PeerCommand)
	registrar.SetFolderCommandHandler(handlers.FolderCommand)
	registrar.SetDiscoveryCommandHandler(handlers.DiscoveryCommand)
	registrar.SetConfigReadHandler(handlers.ConfigRead)
	registrar.SetAPITrustHandler(handlers.APITrust)
	registrar.SetAPITrustCommandHandler(handlers.APITrustCommand)
	registrar.SetIdentityPackageHandler(handlers.IdentityPackage)
	registrar.SetIdentityImportHandler(handlers.IdentityImport)
	registrar.SetFilesystemBrowseHandler(handlers.FilesystemBrowse)
	registrar.SetConfigUpdateHandler(handlers.ConfigUpdate)
	registrar.SetServiceCommandHandler(handlers.ServiceCommand)
	registrar.SetTransferCommandHandler(handlers.TransferCommand)
	registrar.SetWebGUICommandHandler(handlers.WebGUICommand)
}
