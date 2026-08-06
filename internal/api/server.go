package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/pairing"
	"filesyncengine/internal/scanner"
	"filesyncengine/internal/state"
)

type State struct {
	NodeName      string           `json:"nodeName"`
	StartedAt     time.Time        `json:"startedAt"`
	ConfigPath    string           `json:"configPath"`
	ConfigVersion uint64           `json:"configVersion"`
	Folders       int              `json:"folders"`
	Peers         int              `json:"peers"`
	Status        string           `json:"status"`
	Maintenance   MaintenanceState `json:"maintenance"`
	Backup        BackupState      `json:"backup"`
	FoldersState  []FolderState    `json:"-"`
	PeersState    []PeerState      `json:"-"`
}

type MaintenanceState struct {
	Enabled         bool                 `json:"enabled"`
	LastManualScrub *MaintenanceRunState `json:"lastManualScrub,omitempty"`
}

type BackupState struct {
	Enabled     bool                 `json:"enabled"`
	Mode        string               `json:"mode,omitempty"`
	Snapshots   BackupSnapshotState  `json:"snapshots,omitempty"`
	LastRestore *RestoreRunState     `json:"lastRestore,omitempty"`
	LastScrub   *BackupScrubRunState `json:"lastScrub,omitempty"`
}

type BackupScrubRunState struct {
	StartedAt            time.Time `json:"startedAt"`
	FinishedAt           time.Time `json:"finishedAt"`
	ArchiveCheckedJobs   int       `json:"archiveCheckedJobs"`
	ArchiveMissingBlocks int       `json:"archiveMissingBlocks"`
	ArchiveCorruptBlocks int       `json:"archiveCorruptBlocks"`
	ArchiveOrphanBlocks  int       `json:"archiveOrphanBlocks"`
	CheckpointSnapshots  int       `json:"checkpointSnapshots"`
	DegradedSnapshots    int       `json:"degradedSnapshots"`
	RepairableBlocks     int       `json:"repairableBlocks"`
	UnresolvedBlocks     int       `json:"unresolvedBlocks"`
	Message              string    `json:"message,omitempty"`
}

type RestoreRunState struct {
	StartedAt      time.Time `json:"startedAt"`
	FinishedAt     time.Time `json:"finishedAt"`
	SnapshotID     string    `json:"snapshotId"`
	FolderID       string    `json:"folderId"`
	Destination    string    `json:"destination"`
	TotalFiles     int       `json:"totalFiles"`
	RestoredFiles  int       `json:"restoredFiles"`
	RestoredBytes  int64     `json:"restoredBytes"`
	SkippedFiles   int       `json:"skippedFiles"`
	RemainingFiles int       `json:"remainingFiles"`
	Message        string    `json:"message,omitempty"`
}

type BackupSnapshotState struct {
	TotalSnapshots            int                                   `json:"totalSnapshots"`
	MetadataSnapshots         int                                   `json:"metadataSnapshots"`
	ArchiveProtectedSnapshots int                                   `json:"archiveProtectedSnapshots"`
	DBCheckpointSnapshots     int                                   `json:"dbCheckpointSnapshots"`
	Items                     map[string]BackupSnapshotAvailability `json:"items,omitempty"`
}

type BackupSnapshotAvailability struct {
	SnapshotID            string                    `json:"snapshotId"`
	FolderID              string                    `json:"folderId"`
	MetadataPresent       bool                      `json:"metadataPresent"`
	DBCheckpointAvailable bool                      `json:"dbCheckpointAvailable"`
	ArchiveFullyProtected bool                      `json:"archiveFullyProtected"`
	Archive               BackupArchiveAvailability `json:"archive"`
}

type BackupArchiveAvailability struct {
	TotalBlocks          int `json:"totalBlocks"`
	ProtectedBlocks      int `json:"protectedBlocks"`
	PendingBlocks        int `json:"pendingBlocks"`
	FailedBlocks         int `json:"failedBlocks"`
	MissingArchiveBlocks int `json:"missingArchiveBlocks"`
}

type MaintenanceRunState struct {
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt"`
	Folders      int       `json:"folders"`
	FilesScanned int       `json:"filesScanned"`
	BytesScanned int64     `json:"bytesScanned"`
	Reported     int       `json:"reported"`
	Quarantined  int       `json:"quarantined"`
	Complete     bool      `json:"complete"`
	Message      string    `json:"message,omitempty"`
}

type MaintenanceScrubRequest struct {
	FolderID string `json:"folderId,omitempty"`
}

type MaintenanceScrubResponse struct {
	StartedAt    time.Time                      `json:"startedAt"`
	FinishedAt   time.Time                      `json:"finishedAt"`
	Folders      int                            `json:"folders"`
	FilesScanned int                            `json:"filesScanned"`
	BytesScanned int64                          `json:"bytesScanned"`
	Reported     int                            `json:"reported"`
	Quarantined  int                            `json:"quarantined"`
	Complete     bool                           `json:"complete"`
	Results      []MaintenanceScrubFolderResult `json:"results"`
}

type MaintenanceScrubFolderResult struct {
	FolderID     string `json:"folderId"`
	Mode         string `json:"mode"`
	FilesScanned int    `json:"filesScanned"`
	BytesScanned int64  `json:"bytesScanned"`
	Reported     int    `json:"reported"`
	Quarantined  int    `json:"quarantined"`
	Complete     bool   `json:"complete"`
	Yielded      bool   `json:"yielded"`
	Cursor       uint64 `json:"cursor"`
}

type SnapshotRequest struct {
	Action      string `json:"action"`
	ID          string `json:"id,omitempty"`
	FolderID    string `json:"folderId,omitempty"`
	Description string `json:"description,omitempty"`
}

type SnapshotMarker struct {
	ID          string `json:"id"`
	FolderID    string `json:"folderId"`
	Cursor      uint64 `json:"cursor"`
	StateHash   string `json:"stateHash"`
	CreatedAt   string `json:"createdAt"`
	Description string `json:"description,omitempty"`
	Pinned      bool   `json:"pinned,omitempty"`
	Deprecated  bool   `json:"deprecated,omitempty"`
}

type SnapshotResponse struct {
	Markers []SnapshotMarker `json:"markers"`
}

type RestorePlanRequest struct {
	SnapshotID      string   `json:"snapshotId"`
	Paths           []string `json:"paths,omitempty"`
	DestinationRoot string   `json:"destinationRoot,omitempty"`
	AlternatePath   string   `json:"alternatePath,omitempty"`
}

type RestorePlanResponse struct {
	SnapshotID    string            `json:"snapshotId"`
	FolderID      string            `json:"folderId"`
	Destination   string            `json:"destination"`
	DryRun        bool              `json:"dryRun"`
	TotalFiles    int               `json:"totalFiles"`
	TotalBytes    int64             `json:"totalBytes"`
	MissingBlocks int               `json:"missingBlocks"`
	Files         []RestorePlanFile `json:"files"`
}

type RestorePlanFile struct {
	Path             string        `json:"path"`
	DestinationPath  string        `json:"destinationPath"`
	Size             int64         `json:"size"`
	Blocks           int           `json:"blocks"`
	ArchiveAvailable bool          `json:"archiveAvailable"`
	MissingBlocks    []block.Block `json:"missingBlocks,omitempty"`
}

type RestoreRequest struct {
	SnapshotID      string   `json:"snapshotId"`
	Paths           []string `json:"paths,omitempty"`
	DestinationRoot string   `json:"destinationRoot,omitempty"`
	AlternatePath   string   `json:"alternatePath,omitempty"`
	RevertDatabase  bool     `json:"revertDatabase,omitempty"`
}

type RestoreResponse struct {
	StartedAt      time.Time `json:"startedAt"`
	FinishedAt     time.Time `json:"finishedAt"`
	JobID          string    `json:"jobId,omitempty"`
	SnapshotID     string    `json:"snapshotId"`
	FolderID       string    `json:"folderId"`
	Destination    string    `json:"destination"`
	TotalFiles     int       `json:"totalFiles"`
	RestoredFiles  int       `json:"restoredFiles"`
	RestoredBytes  int64     `json:"restoredBytes"`
	SkippedFiles   int       `json:"skippedFiles"`
	RemainingFiles int       `json:"remainingFiles"`
}

type SnapshotRetentionRequest struct {
	KeepLast int `json:"keepLast"`
}

type SnapshotRetentionResponse struct {
	StartedAt           time.Time `json:"startedAt"`
	FinishedAt          time.Time `json:"finishedAt"`
	JobID               string    `json:"jobId,omitempty"`
	KeepLast            int       `json:"keepLast"`
	DeprecatedSnapshots []string  `json:"deprecatedSnapshots"`
	DeletedSnapshots    []string  `json:"deletedSnapshots"`
	PromotedManifests   int       `json:"promotedManifests"`
	SweepEligibleBlocks int       `json:"sweepEligibleBlocks"`
}

type BackupScrubRequest struct{}

type BackupJobsRequest struct {
	SnapshotID string `json:"snapshotId,omitempty"`
}

type BackupJobsResponse struct {
	RestoreJobs   []state.BackupRestoreJob   `json:"restoreJobs"`
	RetentionJobs []state.BackupRetentionJob `json:"retentionJobs"`
	RepairJobs    []state.BackupRepairJob    `json:"repairJobs"`
}

type PeerCommandRequest struct {
	Action   string `json:"action"`
	ID       string `json:"id"`
	Endpoint string `json:"endpoint,omitempty"`
}

type PeerCommandResponse struct {
	Action  string `json:"action"`
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type FolderCommandRequest struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Path   string `json:"path,omitempty"`
	Mode   string `json:"mode,omitempty"`
}

type FolderCommandResponse struct {
	Action  string `json:"action"`
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type DiscoveryCommandRequest struct {
	Action            string                    `json:"action"`
	Disabled          bool                      `json:"disabled"`
	DHT               bool                      `json:"dht"`
	Local             bool                      `json:"local"`
	DHTNamespace      string                    `json:"dhtNamespace,omitempty"`
	DHTBootstrapPeers []string                  `json:"dhtBootstrapPeers,omitempty"`
	NetworkHints      config.NetworkHintsConfig `json:"networkHints,omitempty"`
}

type DiscoveryCommandResponse struct {
	Action  string `json:"action"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ServiceCommandRequest struct {
	Action      string `json:"action"`
	Platform    string `json:"platform"`
	ServiceName string `json:"serviceName"`
	Domain      string `json:"domain,omitempty"`
}

type ServiceCommandResponse struct {
	Action      string `json:"action"`
	Platform    string `json:"platform"`
	ServiceName string `json:"serviceName"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	Handoff     string `json:"handoff,omitempty"`
}

type TransferCommandRequest struct {
	Action   string `json:"action"`
	FolderID string `json:"folderID,omitempty"`
	PeerID   string `json:"peerID,omitempty"`
}

type TransferCommandResponse struct {
	Action   string `json:"action"`
	FolderID string `json:"folderID,omitempty"`
	PeerID   string `json:"peerID,omitempty"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

type WebGUICommandRequest struct {
	Action string `json:"action"`
}

type WebGUICommandResponse struct {
	Action      string `json:"action"`
	Status      string `json:"status"`
	Version     string `json:"version,omitempty"`
	InstallDir  string `json:"installDir,omitempty"`
	Listen      string `json:"listen,omitempty"`
	URL         string `json:"url,omitempty"`
	HTTPSListen string `json:"httpsListen,omitempty"`
	HTTPSURL    string `json:"httpsUrl,omitempty"`
	Running     bool   `json:"running,omitempty"`
	Message     string `json:"message,omitempty"`
}

type IdentityPackageRequest struct {
	GroupID string `json:"groupId"`
}

type IdentityImportRequest struct {
	Package pairing.IdentityPackage `json:"package"`
}

type IdentityImportResponse struct {
	Status                       string `json:"status"`
	Message                      string `json:"message,omitempty"`
	GroupID                      string `json:"groupId"`
	RemoteDiscoveryID            string `json:"remoteDiscoveryId"`
	IntroductionEncryptionLevel  int    `json:"introductionEncryptionLevel"`
	PeerPairEncryptionLevel      int    `json:"peerPairEncryptionLevel"`
	RequiresDedicatedPeerPairKey bool   `json:"requiresDedicatedPeerPairKey"`
	UsesBootstrapKeyForTraffic   bool   `json:"usesBootstrapKeyForTraffic"`
	PairID                       string `json:"pairId,omitempty"`
	KeyID                        string `json:"keyId,omitempty"`
}

type APITrustResponse struct {
	Mode                         string `json:"mode"`
	TLSEnabled                   bool   `json:"tlsEnabled"`
	TLSRequired                  bool   `json:"tlsRequired"`
	CertificateSHA256            string `json:"certificateSha256,omitempty"`
	TrustedCertificateSHA256     string `json:"trustedCertificateSha256,omitempty"`
	TrustedCertificateConfigured bool   `json:"trustedCertificateConfigured"`
	TrustedCertificateMatches    bool   `json:"trustedCertificateMatches"`
	Message                      string `json:"message,omitempty"`
}

type APITrustCommandRequest struct {
	Action string `json:"action"`
}

type APITrustCommandResponse struct {
	Action                       string `json:"action"`
	Status                       string `json:"status"`
	CertificateSHA256            string `json:"certificateSha256,omitempty"`
	TrustedCertificateConfigured bool   `json:"trustedCertificateConfigured"`
	TrustedCertificateMatches    bool   `json:"trustedCertificateMatches"`
	Message                      string `json:"message,omitempty"`
}

type ConfigUpdateRequest struct {
	NodeName    *string                   `json:"nodeName,omitempty"`
	Listen      []string                  `json:"listen,omitempty"`
	API         *ConfigAPIUpdate          `json:"api,omitempty"`
	Logging     *config.LoggingConfig     `json:"logging,omitempty"`
	Transfer    *config.TransferConfig    `json:"transfer,omitempty"`
	Backup      *config.BackupConfig      `json:"backup,omitempty"`
	Discovery   *config.DiscoveryConfig   `json:"discovery,omitempty"`
	Metadata    *config.MetadataConfig    `json:"metadata,omitempty"`
	Maintenance *config.MaintenanceConfig `json:"maintenance,omitempty"`
}

type ConfigAPIUpdate struct {
	Listen     *string                     `json:"listen,omitempty"`
	Encryption *config.APIEncryptionConfig `json:"encryption,omitempty"`
}

func rejectSecretConfigPatchFields(body []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return fmt.Errorf("invalid config update request")
	}
	for _, field := range []string{"identity", "peers", "folders", "apiKey"} {
		if _, ok := root[field]; ok {
			return fmt.Errorf("config update cannot modify secret or identity-bearing field %q", field)
		}
	}
	if rawAPI, ok := root["api"]; ok {
		var apiPatch map[string]json.RawMessage
		if err := json.Unmarshal(rawAPI, &apiPatch); err != nil {
			return fmt.Errorf("invalid config update request")
		}
		for _, field := range []string{"key", "apiKey"} {
			if _, ok := apiPatch[field]; ok {
				return fmt.Errorf("config update cannot modify secret api.%s", field)
			}
		}
	}
	return nil
}

type ConfigUpdateResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type BackupScrubResponse struct {
	StartedAt   time.Time                  `json:"startedAt"`
	FinishedAt  time.Time                  `json:"finishedAt"`
	Archive     BackupArchiveScrubState    `json:"archive"`
	Checkpoints BackupCheckpointScrubState `json:"checkpoints"`
	RepairPlan  BackupRepairPlanState      `json:"repairPlan"`
}

type BackupArchiveScrubState struct {
	CheckedJobs     int `json:"checkedJobs"`
	ProtectedBlocks int `json:"protectedBlocks"`
	MissingBlocks   int `json:"missingBlocks"`
	CorruptBlocks   int `json:"corruptBlocks"`
	IncompleteJobs  int `json:"incompleteJobs"`
	OrphanBlocks    int `json:"orphanBlocks"`
	Issues          int `json:"issues"`
}

type BackupCheckpointScrubState struct {
	CheckedSnapshots     int `json:"checkedSnapshots"`
	AvailableCheckpoints int `json:"availableCheckpoints"`
	MissingCheckpoints   int `json:"missingCheckpoints"`
	CorruptCheckpoints   int `json:"corruptCheckpoints"`
	DegradedSnapshots    int `json:"degradedSnapshots"`
	Issues               int `json:"issues"`
}

type BackupRepairPlanState struct {
	RepairableBlocks int `json:"repairableBlocks"`
	UnresolvedBlocks int `json:"unresolvedBlocks"`
	Actions          int `json:"actions"`
	Unresolved       int `json:"unresolved"`
}

type MaintenanceScrubHandler func(context.Context, MaintenanceScrubRequest) (MaintenanceScrubResponse, error)
type SnapshotHandler func(context.Context, SnapshotRequest) (SnapshotResponse, error)
type RestorePlanHandler func(context.Context, RestorePlanRequest) (RestorePlanResponse, error)
type RestoreHandler func(context.Context, RestoreRequest) (RestoreResponse, error)
type SnapshotRetentionHandler func(context.Context, SnapshotRetentionRequest) (SnapshotRetentionResponse, error)
type BackupScrubHandler func(context.Context, BackupScrubRequest) (BackupScrubResponse, error)
type BackupJobsHandler func(context.Context, BackupJobsRequest) (BackupJobsResponse, error)
type PeerCommandHandler func(context.Context, PeerCommandRequest) (PeerCommandResponse, error)
type FolderCommandHandler func(context.Context, FolderCommandRequest) (FolderCommandResponse, error)
type DiscoveryCommandHandler func(context.Context, DiscoveryCommandRequest) (DiscoveryCommandResponse, error)
type ServiceCommandHandler func(context.Context, ServiceCommandRequest) (ServiceCommandResponse, error)
type TransferCommandHandler func(context.Context, TransferCommandRequest) (TransferCommandResponse, error)
type WebGUICommandHandler func(context.Context, WebGUICommandRequest) (WebGUICommandResponse, error)
type IdentityPackageHandler func(context.Context, IdentityPackageRequest) (pairing.IdentityPackage, error)
type IdentityImportHandler func(context.Context, IdentityImportRequest) (IdentityImportResponse, error)
type APITrustHandler func(context.Context) (APITrustResponse, error)
type APITrustCommandHandler func(context.Context, APITrustCommandRequest) (APITrustCommandResponse, error)
type MeshSettingsHandler func(context.Context, MeshSettingsRequest) (MeshSettingsResponse, error)
type MeshSettingsCommandHandler func(context.Context, MeshSettingsCommandRequest) (MeshSettingsCommandResponse, error)
type FilesystemBrowseHandler func(context.Context, FilesystemBrowseRequest) (FilesystemBrowseResponse, error)
type ConfigReadHandler func(context.Context) (config.Config, error)

// FilesystemBrowseRequest asks the selected daemon host to enumerate one local directory for
// GUI folder pickers. The API deliberately returns directory entries only so clients do not
// mistake ordinary files for selectable sync roots.
type FilesystemBrowseRequest struct {
	Path string `json:"path"`
}

type FilesystemBrowseResponse struct {
	Path    string                  `json:"path"`
	Entries []FilesystemBrowseEntry `json:"entries"`
}

type FilesystemBrowseEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Readable bool   `json:"readable"`
}
type ConfigUpdateHandler func(context.Context, ConfigUpdateRequest) (ConfigUpdateResponse, error)
type StopHandler func(context.Context) error

type MeshSettingsRequest struct {
	NodeID string `json:"nodeId,omitempty"`
}

type MeshSettingsResponse struct {
	Documents []state.NodeSettingsDocument `json:"documents"`
}

type MeshSettingsCommandRequest struct {
	Action         string         `json:"action"`
	TargetNodeID   string         `json:"targetNodeId"`
	OriginNodeID   string         `json:"originNodeId"`
	IdempotencyKey string         `json:"idempotencyKey"`
	SettingsPatch  map[string]any `json:"settingsPatch,omitempty"`
}

type MeshSettingsCommandResponse struct {
	Action         string `json:"action"`
	Status         string `json:"status"`
	ChangeID       string `json:"changeId"`
	TargetNodeID   string `json:"targetNodeId"`
	OriginNodeID   string `json:"originNodeId"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Message        string `json:"message,omitempty"`
}

type FolderState struct {
	ID       string             `json:"id"`
	Path     string             `json:"path"`
	Mode     string             `json:"mode"`
	Status   string             `json:"status"`
	Index    FolderIndexState   `json:"index"`
	Sync     FolderSyncState    `json:"sync"`
	Warnings FolderWarningState `json:"warnings"`
}

type FolderIndexState struct {
	Mode                   string `json:"mode"`
	TotalFiles             int    `json:"totalFiles"`
	VerifiedFiles          int    `json:"verifiedFiles"`
	UnknownFiles           int    `json:"unknownFiles"`
	UnverifiedSeedFiles    int    `json:"unverifiedSeedFiles"`
	KnownBlocks            int    `json:"knownBlocks"`
	BadBlocks              int    `json:"badBlocks"`
	QueuedHashJobs         int    `json:"queuedHashJobs"`
	ActiveHashJobs         int    `json:"activeHashJobs"`
	DateCorrectionsPending int    `json:"dateCorrectionsPending"`
	ProvisionalReadOnly    bool   `json:"provisionalReadOnly"`
}

type FolderSyncState struct {
	LocalCursor            uint64 `json:"localCursor"`
	LocalStateHash         string `json:"localStateHash"`
	DeferredDeletes        int    `json:"deferredDeletes"`
	ReadyDeferredDeletes   int    `json:"readyDeferredDeletes"`
	MetadataCatchupPending bool   `json:"metadataCatchupPending"`
}

type FolderWarningState struct {
	InaccessibleFiles    int             `json:"inaccessibleFiles"`
	PendingLockedApplies int             `json:"pendingLockedApplies"`
	Recent               []FolderWarning `json:"recent,omitempty"`
}

type FolderWarning struct {
	Kind    string               `json:"kind"`
	Path    string               `json:"path"`
	Message string               `json:"message"`
	Repair  *FolderRepairWarning `json:"repair,omitempty"`
}

type FolderRepairWarning struct {
	Status              string `json:"status"`
	RestoredCopyInPlace bool   `json:"restoredCopyInPlace"`
	OriginalAvailable   bool   `json:"originalAvailable"`
	QuarantinePath      string `json:"quarantinePath,omitempty"`
	UserAction          string `json:"userAction,omitempty"`
}

type PeerState struct {
	ID                 string                  `json:"id"`
	Status             string                  `json:"status"`
	Endpoint           string                  `json:"endpoint,omitempty"`
	Transfer           PeerTransferState       `json:"transfer,omitempty"`
	Metadata           PeerMetadataState       `json:"metadata,omitempty"`
	NetworkDiagnostics []PeerNetworkDiagnostic `json:"networkDiagnostics,omitempty"`
}

type PeerNetworkDiagnostic struct {
	Code      string `json:"code"`
	Address   string `json:"address,omitempty"`
	RoutePath string `json:"routePath,omitempty"`
	Network   string `json:"network,omitempty"`
	Guidance  string `json:"guidance"`
}

type PeerTransferState struct {
	Configured   config.EffectiveTransferConfig `json:"configured"`
	Effective    config.EffectiveTransferConfig `json:"effective"`
	SendCause    string                         `json:"sendCause,omitempty"`
	ReceiveCause string                         `json:"receiveCause,omitempty"`
}

type PeerMetadataState struct {
	Folders []PeerFolderMetadataStatus `json:"folders,omitempty"`
}

type PeerFolderMetadataStatus struct {
	FolderID       string `json:"folderId"`
	PeerCursor     uint64 `json:"peerCursor"`
	PeerStateHash  string `json:"peerStateHash"`
	LocalCursor    uint64 `json:"localCursor"`
	LocalStateHash string `json:"localStateHash"`
	InSync         bool   `json:"inSync"`
}

type Event struct {
	Type     string         `json:"type"`
	Time     time.Time      `json:"time"`
	FolderID string         `json:"folderID,omitempty"`
	PeerID   string         `json:"peerID,omitempty"`
	Path     string         `json:"path,omitempty"`
	Message  string         `json:"message,omitempty"`
	Progress *ProgressState `json:"progress,omitempty"`
}

type LogsResponse struct {
	Entries []Event `json:"entries"`
}

// ActionableErrorsResponse is a browser-safe operational summary. It deliberately
// excludes event messages, paths, peer IDs, folder IDs, and credentials because
// those diagnostic details can contain private deployment information.
type ActionableErrorsResponse struct {
	Errors []ActionableError `json:"errors"`
}

type ActionableError struct {
	Kind   string `json:"kind"`
	Action string `json:"action"`
	Count  int    `json:"count"`
}

type ProgressState struct {
	QueuedHashJobs         int `json:"queuedHashJobs"`
	ActiveHashJobs         int `json:"activeHashJobs"`
	CompletedHashJobs      int `json:"completedHashJobs"`
	FailedHashJobs         int `json:"failedHashJobs"`
	DateCorrectionsPending int `json:"dateCorrectionsPending"`
	RepairQueuedBlocks     int `json:"repairQueuedBlocks"`
	RepairCompletedBlocks  int `json:"repairCompletedBlocks"`
	BadBlocks              int `json:"badBlocks"`
}

type Server struct {
	state                      State
	apiKey                     string
	maintenanceScrubHandler    MaintenanceScrubHandler
	snapshotHandler            SnapshotHandler
	restorePlanHandler         RestorePlanHandler
	restoreHandler             RestoreHandler
	snapshotRetentionHandler   SnapshotRetentionHandler
	backupScrubHandler         BackupScrubHandler
	backupJobsHandler          BackupJobsHandler
	peerCommandHandler         PeerCommandHandler
	folderCommandHandler       FolderCommandHandler
	discoveryCommandHandler    DiscoveryCommandHandler
	serviceCommandHandler      ServiceCommandHandler
	transferCommandHandler     TransferCommandHandler
	webGUICommandHandler       WebGUICommandHandler
	identityPackageHandler     IdentityPackageHandler
	identityImportHandler      IdentityImportHandler
	apiTrustHandler            APITrustHandler
	apiTrustCommandHandler     APITrustCommandHandler
	meshSettingsHandler        MeshSettingsHandler
	meshSettingsCommandHandler MeshSettingsCommandHandler
	filesystemBrowseHandler    FilesystemBrowseHandler
	configReadHandler          ConfigReadHandler
	configUpdateHandler        ConfigUpdateHandler
	stopHandler                StopHandler
	mu                         sync.RWMutex
	events                     []Event
	subscribers                map[chan Event]struct{}
}

func NewServer(state State, apiKey string) *Server {
	if state.Status == "" {
		state.Status = "starting"
	}
	return &Server{state: state, apiKey: apiKey, subscribers: map[chan Event]struct{}{}}
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.requireKey(s.handleStatus))
	mux.HandleFunc("/v1/logs", s.requireKey(s.handleLogs))
	mux.HandleFunc("/v1/actionable-errors", s.requireKey(s.handleActionableErrors))
	mux.HandleFunc("/v1/events", s.requireKey(s.handleEvents))
	mux.HandleFunc("/v1/folders", s.requireKey(s.handleFolders))
	mux.HandleFunc("/v1/peers", s.requireKey(s.handlePeers))
	mux.HandleFunc("/v1/transfers", s.requireKey(s.handleTransfers))
	mux.HandleFunc("/v1/maintenance/scrub", s.requireKey(s.handleMaintenanceScrub))
	mux.HandleFunc("/v1/snapshots", s.requireKey(s.handleSnapshots))
	mux.HandleFunc("/v1/restore-plans", s.requireKey(s.handleRestorePlans))
	mux.HandleFunc("/v1/restores", s.requireKey(s.handleRestores))
	mux.HandleFunc("/v1/snapshot-retention", s.requireKey(s.handleSnapshotRetention))
	mux.HandleFunc("/v1/backup/scrub", s.requireKey(s.handleBackupScrub))
	mux.HandleFunc("/v1/backup/jobs", s.requireKey(s.handleBackupJobs))
	mux.HandleFunc("/v1/peer-command", s.requireKey(s.handlePeerCommand))
	mux.HandleFunc("/v1/folder-command", s.requireKey(s.handleFolderCommand))
	mux.HandleFunc("/v1/discovery-command", s.requireKey(s.handleDiscoveryCommand))
	mux.HandleFunc("/v1/service-command", s.requireKey(s.handleServiceCommand))
	mux.HandleFunc("/v1/transfer-command", s.requireKey(s.handleTransferCommand))
	mux.HandleFunc("/v1/web-gui-command", s.requireKey(s.handleWebGUICommand))
	mux.HandleFunc("/v1/identity-package", s.requireKey(s.handleIdentityPackage))
	mux.HandleFunc("/v1/identity-import", s.requireKey(s.handleIdentityImport))
	mux.HandleFunc("/v1/api/trust", s.requireKey(s.handleAPITrust))
	mux.HandleFunc("/v1/api/trust-command", s.requireKey(s.handleAPITrustCommand))
	mux.HandleFunc("/v1/mesh/settings", s.requireKey(s.handleMeshSettings))
	mux.HandleFunc("/v1/mesh/settings-command", s.requireKey(s.handleMeshSettingsCommand))
	mux.HandleFunc("/v1/filesystem/browse", s.requireKey(s.handleFilesystemBrowse))
	mux.HandleFunc("/v1/config", s.requireKey(s.handleConfig))
	mux.HandleFunc("/v1/stop", s.requireKey(s.handleStop))
	mux.HandleFunc("/v1/folder-index", s.requireKey(s.handleFolderIndex))
	mux.HandleFunc("/v1/folder-file", s.requireKey(s.handleFolderFile))
	mux.HandleFunc("/v1/folder-block", s.requireKey(s.handleFolderBlock))
	return mux
}

func (s *Server) SetMaintenanceScrubHandler(handler MaintenanceScrubHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maintenanceScrubHandler = handler
}

func (s *Server) SetSnapshotHandler(handler SnapshotHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotHandler = handler
}

func (s *Server) SetRestorePlanHandler(handler RestorePlanHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restorePlanHandler = handler
}

func (s *Server) SetRestoreHandler(handler RestoreHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restoreHandler = handler
}

func (s *Server) SetSnapshotRetentionHandler(handler SnapshotRetentionHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotRetentionHandler = handler
}

func (s *Server) SetBackupScrubHandler(handler BackupScrubHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backupScrubHandler = handler
}

func (s *Server) SetBackupJobsHandler(handler BackupJobsHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backupJobsHandler = handler
}

func (s *Server) SetPeerCommandHandler(handler PeerCommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peerCommandHandler = handler
}

func (s *Server) SetFolderCommandHandler(handler FolderCommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.folderCommandHandler = handler
}

func (s *Server) SetDiscoveryCommandHandler(handler DiscoveryCommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discoveryCommandHandler = handler
}

func (s *Server) SetServiceCommandHandler(handler ServiceCommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serviceCommandHandler = handler
}

func (s *Server) SetTransferCommandHandler(handler TransferCommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transferCommandHandler = handler
}

func (s *Server) SetWebGUICommandHandler(handler WebGUICommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webGUICommandHandler = handler
}

func (s *Server) SetIdentityPackageHandler(handler IdentityPackageHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identityPackageHandler = handler
}

func (s *Server) SetIdentityImportHandler(handler IdentityImportHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identityImportHandler = handler
}

func (s *Server) SetAPITrustHandler(handler APITrustHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiTrustHandler = handler
}

func (s *Server) SetAPITrustCommandHandler(handler APITrustCommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiTrustCommandHandler = handler
}

func (s *Server) SetMeshSettingsHandler(handler MeshSettingsHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meshSettingsHandler = handler
}

func (s *Server) SetMeshSettingsCommandHandler(handler MeshSettingsCommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meshSettingsCommandHandler = handler
}

func (s *Server) SetFilesystemBrowseHandler(handler FilesystemBrowseHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filesystemBrowseHandler = handler
}

func (s *Server) SetConfigReadHandler(handler ConfigReadHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configReadHandler = handler
}

func (s *Server) SetConfigUpdateHandler(handler ConfigUpdateHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configUpdateHandler = handler
}

func (s *Server) SetStopHandler(handler StopHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopHandler = handler
}

func (s *Server) Publish(event Event) {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	s.mu.Lock()
	s.events = append(s.events, event)
	if len(s.events) > 256 {
		s.events = s.events[len(s.events)-256:]
	}
	subscribers := make([]chan Event, 0, len(s.subscribers))
	for ch := range s.subscribers {
		subscribers = append(subscribers, ch)
	}
	s.mu.Unlock()
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Server) UpdateState(state State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

func (s *Server) CurrentState() State {
	return s.snapshotState()
}

func (s *Server) requireKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey == "" || r.Header.Get("X-FSE-API-Key") != s.apiKey {
			http.Error(w, "missing or invalid API key", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.snapshotState())
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			http.Error(w, "invalid log limit", http.StatusBadRequest)
			return
		}
		if parsed < limit {
			limit = parsed
		}
	}
	s.mu.RLock()
	entries := append([]Event(nil), s.events...)
	s.mu.RUnlock()
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	writeJSON(w, LogsResponse{Entries: entries})
}

func (s *Server) handleActionableErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	events := append([]Event(nil), s.events...)
	s.mu.RUnlock()

	errors := make([]ActionableError, 0)
	indexes := map[string]int{}
	for _, event := range events {
		kind, action, ok := actionableErrorSummary(event.Type)
		if !ok {
			continue
		}
		if index, exists := indexes[kind]; exists {
			errors[index].Count++
			continue
		}
		indexes[kind] = len(errors)
		errors = append(errors, ActionableError{Kind: kind, Action: action, Count: 1})
	}
	writeJSON(w, ActionableErrorsResponse{Errors: errors})
}

func actionableErrorSummary(eventType string) (kind, action string, ok bool) {
	switch eventType {
	case "sync.error":
		return "sync", "Review the daemon logs and folder access on the server.", true
	case "peer.sync.error":
		return "peer_sync", "Check peer reachability and the peer configuration on the server.", true
	case "watch.error", "folder.warning":
		return "folder", "Review folder access and pending-write warnings on the server.", true
	case "discovery.error":
		return "discovery", "Review discovery settings and network reachability on the server.", true
	case "metadata.catchup.error":
		return "metadata", "Review metadata synchronization and peer availability on the server.", true
	case "webgui.startup.failed":
		return "web_gui", "Review the optional web GUI delivery and listener configuration on the server.", true
	default:
		return "", "", false
	}
}

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	state := s.snapshotState()
	writeJSON(w, state.FoldersState)
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	state := s.snapshotState()
	writeJSON(w, state.PeersState)
}

func (s *Server) handleMaintenanceScrub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.maintenanceScrubHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "maintenance scrub trigger is not configured", http.StatusServiceUnavailable)
		return
	}
	var req MaintenanceScrubRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid maintenance scrub request", http.StatusBadRequest)
			return
		}
	}
	started := time.Now().UTC()
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.StartedAt.IsZero() {
		response.StartedAt = started
	}
	if response.FinishedAt.IsZero() {
		response.FinishedAt = time.Now().UTC()
	}
	response.recalculateSummary()
	s.updateMaintenanceScrubState(response)
	writeJSON(w, response)
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.snapshotHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "snapshot handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req SnapshotRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid snapshot request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (s *Server) handleRestorePlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.restorePlanHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "restore plan handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req RestorePlanRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid restore plan request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (s *Server) handleRestores(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.restoreHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "restore handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req RestoreRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid restore request", http.StatusBadRequest)
			return
		}
	}
	if req.RevertDatabase {
		http.Error(w, "database reversion requires a dedicated rollback flow; ordinary snapshot restore only writes verified files", http.StatusBadRequest)
		return
	}
	started := time.Now().UTC()
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.StartedAt.IsZero() {
		response.StartedAt = started
	}
	if response.FinishedAt.IsZero() {
		response.FinishedAt = time.Now().UTC()
	}
	s.updateRestoreState(response)
	writeJSON(w, response)
}

func (s *Server) handleSnapshotRetention(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.snapshotRetentionHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "snapshot retention handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req SnapshotRetentionRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid snapshot retention request", http.StatusBadRequest)
			return
		}
	}
	if req.KeepLast < 1 {
		http.Error(w, "keepLast must be at least 1", http.StatusBadRequest)
		return
	}
	started := time.Now().UTC()
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.StartedAt.IsZero() {
		response.StartedAt = started
	}
	if response.FinishedAt.IsZero() {
		response.FinishedAt = time.Now().UTC()
	}
	s.publishSnapshotRetentionFinished(response)
	writeJSON(w, response)
}

func (s *Server) handleBackupScrub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.backupScrubHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "backup scrub trigger is not configured", http.StatusServiceUnavailable)
		return
	}
	started := time.Now().UTC()
	response, err := handler(r.Context(), BackupScrubRequest{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.StartedAt.IsZero() {
		response.StartedAt = started
	}
	if response.FinishedAt.IsZero() {
		response.FinishedAt = time.Now().UTC()
	}
	s.updateBackupScrubState(response)
	writeJSON(w, response)
}

func (s *Server) handleBackupJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.backupJobsHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "backup jobs handler is not configured", http.StatusServiceUnavailable)
		return
	}
	response, err := handler(r.Context(), BackupJobsRequest{SnapshotID: r.URL.Query().Get("snapshotId")})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (s *Server) handlePeerCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.peerCommandHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "peer command handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req PeerCommandRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid peer command request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.Status == "" {
		response.Status = "accepted"
	}
	s.Publish(Event{Type: "peer.command.finished", PeerID: response.ID, Message: fmt.Sprintf("peer command %s finished for %s", response.Action, response.ID)})
	writeJSON(w, response)
}

func (s *Server) handleFolderCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.folderCommandHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "folder command handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req FolderCommandRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid folder command request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.Status == "" {
		response.Status = "accepted"
	}
	s.Publish(Event{Type: "folder.command.finished", FolderID: response.ID, Message: fmt.Sprintf("folder command %s finished for %s", response.Action, response.ID)})
	writeJSON(w, response)
}

func (s *Server) handleDiscoveryCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.discoveryCommandHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "discovery command handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req DiscoveryCommandRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid discovery command request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.Status == "" {
		response.Status = "accepted"
	}
	s.Publish(Event{Type: "discovery.command.finished", Message: fmt.Sprintf("discovery command %s finished", response.Action)})
	writeJSON(w, response)
}

func (s *Server) handleServiceCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.serviceCommandHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "service command handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req ServiceCommandRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid service command request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.Status == "" {
		response.Status = "accepted"
	}
	s.Publish(Event{Type: "service.command.finished", Message: fmt.Sprintf("service command %s finished for %s", response.Action, response.ServiceName)})
	writeJSON(w, response)
}

func (s *Server) handleTransferCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.transferCommandHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "transfer command handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req TransferCommandRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid transfer command request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.Status == "" {
		response.Status = "accepted"
	}
	s.Publish(Event{Type: "transfer.command.finished", FolderID: response.FolderID, PeerID: response.PeerID, Message: fmt.Sprintf("transfer command %s finished", response.Action)})
	writeJSON(w, response)
}

func (s *Server) handleWebGUICommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.webGUICommandHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "web GUI command handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req WebGUICommandRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid web GUI command request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.Status == "" {
		response.Status = "accepted"
	}
	s.Publish(Event{Type: "webgui.command.finished", Message: fmt.Sprintf("web GUI command %s finished", response.Action)})
	writeJSON(w, response)
}

func (s *Server) handleIdentityPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.identityPackageHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "identity package handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req IdentityPackageRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid identity package request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.Publish(Event{Type: "identity.package.generated", Message: fmt.Sprintf("identity package generated for group %s", response.GroupID)})
	writeJSON(w, response)
}

func (s *Server) handleIdentityImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.identityImportHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "identity import handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req IdentityImportRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid identity import request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.Publish(Event{Type: "identity.package.imported", Message: fmt.Sprintf("identity package import accepted for group %s remote %s", response.GroupID, response.RemoteDiscoveryID)})
	writeJSON(w, response)
}

func (s *Server) handleAPITrust(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.apiTrustHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "api trust handler is not configured", http.StatusServiceUnavailable)
		return
	}
	response, err := handler(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (s *Server) handleAPITrustCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.apiTrustCommandHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "api trust command handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req APITrustCommandRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid api trust command request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.Action == "" {
		response.Action = req.Action
	}
	if response.Status == "" {
		response.Status = "accepted"
	}
	s.Publish(Event{Type: "api.trust.pinned", Message: "api trust command accepted"})
	writeJSON(w, response)
}

func (s *Server) handleMeshSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.meshSettingsHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "mesh settings handler is not configured", http.StatusServiceUnavailable)
		return
	}
	response, err := handler(r.Context(), MeshSettingsRequest{NodeID: r.URL.Query().Get("nodeId")})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (s *Server) handleMeshSettingsCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.meshSettingsCommandHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "mesh settings command handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var req MeshSettingsCommandRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			http.Error(w, "invalid mesh settings command request", http.StatusBadRequest)
			return
		}
	}
	response, err := handler(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if response.Status == "" {
		response.Status = "queued"
	}
	if response.Action == "" {
		response.Action = req.Action
	}
	s.Publish(Event{Type: "mesh.settings.command.queued", Message: fmt.Sprintf("mesh settings command %s queued for %s", response.Action, response.TargetNodeID)})
	writeJSON(w, response)
}

func (s *Server) handleFilesystemBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.filesystemBrowseHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "filesystem browse handler is not configured", http.StatusServiceUnavailable)
		return
	}
	response, err := handler(r.Context(), FilesystemBrowseRequest{Path: r.URL.Query().Get("path")})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response.Entries = directoryBrowseEntriesOnly(response.Entries)
	writeJSON(w, response)
}

func directoryBrowseEntriesOnly(entries []FilesystemBrowseEntry) []FilesystemBrowseEntry {
	filtered := make([]FilesystemBrowseEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == "directory" {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func BrowseLocalFilesystem(ctx context.Context, req FilesystemBrowseRequest) (FilesystemBrowseResponse, error) {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		var err error
		path, err = os.UserHomeDir()
		if err != nil || path == "" {
			path = "."
		}
	}
	cleaned, err := filepath.Abs(path)
	if err != nil {
		return FilesystemBrowseResponse{}, err
	}
	entries, err := os.ReadDir(cleaned)
	if err != nil {
		return FilesystemBrowseResponse{}, err
	}
	response := FilesystemBrowseResponse{Path: cleaned, Entries: []FilesystemBrowseEntry{}}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return FilesystemBrowseResponse{}, ctx.Err()
		}
		if !entry.IsDir() {
			continue
		}
		entryPath := filepath.Join(cleaned, entry.Name())
		readable := true
		if _, err := os.ReadDir(entryPath); err != nil {
			readable = false
		}
		response.Entries = append(response.Entries, FilesystemBrowseEntry{Name: entry.Name(), Path: entryPath, Type: "directory", Readable: readable})
	}
	return response, nil
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		handler := s.configReadHandler
		s.mu.RUnlock()
		if handler == nil {
			http.Error(w, "config read handler is not configured", http.StatusServiceUnavailable)
			return
		}
		cfg, err := handler(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data, err := config.RedactedJSON(cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	case http.MethodPatch:
		s.mu.RLock()
		handler := s.configUpdateHandler
		s.mu.RUnlock()
		if handler == nil {
			http.Error(w, "config update handler is not configured", http.StatusServiceUnavailable)
			return
		}
		var req ConfigUpdateRequest
		if r.Body != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid config update request", http.StatusBadRequest)
				return
			}
			if len(strings.TrimSpace(string(body))) > 0 {
				if err := rejectSecretConfigPatchFields(body); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if err := json.Unmarshal(body, &req); err != nil {
					http.Error(w, "invalid config update request", http.StatusBadRequest)
					return
				}
			}
		}
		response, err := handler(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if response.Status == "" {
			response.Status = "accepted"
		}
		s.Publish(Event{Type: "config.command.finished", Message: "config update finished"})
		writeJSON(w, response)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.stopHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "stop handler is not configured", http.StatusServiceUnavailable)
		return
	}
	s.mu.Lock()
	s.state.Status = "stopping"
	s.mu.Unlock()
	s.Publish(Event{Type: "daemon.stopping", Message: "daemon stop requested"})
	if err := handler(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "stopping"})
}

func (r *MaintenanceScrubResponse) recalculateSummary() {
	r.Folders = len(r.Results)
	r.FilesScanned = 0
	r.BytesScanned = 0
	r.Reported = 0
	r.Quarantined = 0
	r.Complete = true
	for _, result := range r.Results {
		r.FilesScanned += result.FilesScanned
		r.BytesScanned += result.BytesScanned
		r.Reported += result.Reported
		r.Quarantined += result.Quarantined
		if !result.Complete {
			r.Complete = false
		}
	}
}

func (s *Server) updateMaintenanceScrubState(response MaintenanceScrubResponse) {
	message := fmt.Sprintf("maintenance scrub finished: folders=%d files=%d bytes=%d reported=%d quarantined=%d complete=%v", response.Folders, response.FilesScanned, response.BytesScanned, response.Reported, response.Quarantined, response.Complete)
	s.mu.Lock()
	s.state.Maintenance.LastManualScrub = &MaintenanceRunState{
		StartedAt:    response.StartedAt,
		FinishedAt:   response.FinishedAt,
		Folders:      response.Folders,
		FilesScanned: response.FilesScanned,
		BytesScanned: response.BytesScanned,
		Reported:     response.Reported,
		Quarantined:  response.Quarantined,
		Complete:     response.Complete,
		Message:      message,
	}
	s.mu.Unlock()
	s.Publish(Event{Type: "maintenance.scrub.finished", Message: message})
}

func (s *Server) updateRestoreState(response RestoreResponse) {
	message := fmt.Sprintf("snapshot restore finished: snapshot=%s folder=%s files=%d restored=%d bytes=%d skipped=%d remaining=%d", response.SnapshotID, response.FolderID, response.TotalFiles, response.RestoredFiles, response.RestoredBytes, response.SkippedFiles, response.RemainingFiles)
	s.mu.Lock()
	s.state.Backup.LastRestore = &RestoreRunState{
		StartedAt:      response.StartedAt,
		FinishedAt:     response.FinishedAt,
		SnapshotID:     response.SnapshotID,
		FolderID:       response.FolderID,
		Destination:    response.Destination,
		TotalFiles:     response.TotalFiles,
		RestoredFiles:  response.RestoredFiles,
		RestoredBytes:  response.RestoredBytes,
		SkippedFiles:   response.SkippedFiles,
		RemainingFiles: response.RemainingFiles,
		Message:        message,
	}
	s.mu.Unlock()
	s.Publish(Event{Type: "snapshot.restore.finished", FolderID: response.FolderID, Message: message})
}

func (s *Server) publishSnapshotRetentionFinished(response SnapshotRetentionResponse) {
	message := fmt.Sprintf("snapshot retention finished: keepLast=%d deprecated=%d deleted=%d promoted=%d sweepEligibleBlocks=%d", response.KeepLast, len(response.DeprecatedSnapshots), len(response.DeletedSnapshots), response.PromotedManifests, response.SweepEligibleBlocks)
	s.Publish(Event{Type: "snapshot.retention.finished", Message: message})
}

func (s *Server) updateBackupScrubState(response BackupScrubResponse) {
	message := fmt.Sprintf("backup scrub finished: archiveIssues=%d checkpointIssues=%d repairable=%d unresolved=%d", response.Archive.Issues, response.Checkpoints.Issues, response.RepairPlan.RepairableBlocks, response.RepairPlan.UnresolvedBlocks)
	s.mu.Lock()
	s.state.Backup.LastScrub = &BackupScrubRunState{
		StartedAt:            response.StartedAt,
		FinishedAt:           response.FinishedAt,
		ArchiveCheckedJobs:   response.Archive.CheckedJobs,
		ArchiveMissingBlocks: response.Archive.MissingBlocks,
		ArchiveCorruptBlocks: response.Archive.CorruptBlocks,
		ArchiveOrphanBlocks:  response.Archive.OrphanBlocks,
		CheckpointSnapshots:  response.Checkpoints.CheckedSnapshots,
		DegradedSnapshots:    response.Checkpoints.DegradedSnapshots,
		RepairableBlocks:     response.RepairPlan.RepairableBlocks,
		UnresolvedBlocks:     response.RepairPlan.UnresolvedBlocks,
		Message:              message,
	}
	s.mu.Unlock()
	s.Publish(Event{Type: "backup.scrub.finished", Message: message})
}

func (s *Server) handleFolderIndex(w http.ResponseWriter, r *http.Request) {
	folder, ok := s.findFolder(r.URL.Query().Get("folder"))
	if !ok {
		http.Error(w, "unknown folder", http.StatusNotFound)
		return
	}
	result, err := scanner.ScanFolder(folder.Path, scanner.Options{BlockSize: 128 * 1024})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleFolderFile(w http.ResponseWriter, r *http.Request) {
	folder, ok := s.findFolder(r.URL.Query().Get("folder"))
	if !ok {
		http.Error(w, "unknown folder", http.StatusNotFound)
		return
	}
	file, ok, err := openSafeFolderFile(folder.Path, r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleFolderBlock(w http.ResponseWriter, r *http.Request) {
	folder, ok := s.findFolder(r.URL.Query().Get("folder"))
	if !ok {
		http.Error(w, "unknown folder", http.StatusNotFound)
		return
	}
	file, ok, err := openSafeFolderFile(folder.Path, r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer file.Close()
	blockSize, err := strconv.Atoi(r.URL.Query().Get("blockSize"))
	if err != nil || blockSize <= 0 {
		http.Error(w, "invalid blockSize", http.StatusBadRequest)
		return
	}
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || index < 0 {
		http.Error(w, "invalid index", http.StatusBadRequest)
		return
	}
	info, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	offset := int64(index * blockSize)
	if offset >= info.Size() {
		http.Error(w, "block index out of range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	remaining := info.Size() - offset
	limit := int64(blockSize)
	if remaining < limit {
		limit = remaining
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := io.CopyN(w, file, limit); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	s.mu.RLock()
	events := append([]Event(nil), s.events...)
	s.mu.RUnlock()
	for _, event := range events {
		writeSSE(w, event)
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	ch := make(chan Event, 16)
	s.addSubscriber(ch)
	defer s.removeSubscriber(ch)
	flusher.Flush()
	for {
		select {
		case event := <-ch:
			writeSSE(w, event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) snapshotState() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Server) findFolder(id string) (FolderState, bool) {
	state := s.snapshotState()
	for _, folder := range state.FoldersState {
		if folder.ID == id {
			return folder, true
		}
	}
	return FolderState{}, false
}

func safeFolderPath(root, rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	cleanPath, err := filepath.Abs(filepath.Join(cleanRoot, cleanRel))
	if err != nil {
		return "", false
	}
	if !pathWithinRoot(cleanRoot, cleanPath) {
		return "", false
	}
	return cleanPath, true
}

func openSafeFolderFile(root, rel string) (http.File, bool, error) {
	path, ok := safeFolderPath(root, rel)
	if !ok {
		return nil, false, nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, false, err
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return nil, false, err
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, true, err
	}
	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return nil, false, err
	}
	if !pathWithinRoot(resolvedRoot, resolvedPath) {
		return nil, false, nil
	}
	relToRoot, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || relToRoot == "." || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) {
		return nil, false, err
	}
	file, err := http.Dir(resolvedRoot).Open(filepath.ToSlash(relToRoot))
	if err != nil {
		return nil, true, err
	}
	return file, true, nil
}

func pathWithinRoot(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func (s *Server) addSubscriber(ch chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers[ch] = struct{}{}
}

func (s *Server) removeSubscriber(ch chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscribers, ch)
	close(ch)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeSSE(w http.ResponseWriter, event Event) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
}
