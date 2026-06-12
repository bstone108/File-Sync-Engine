package ci

import (
	"archive/tar"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMobileGUIScaffoldDefinesSmallScreenFirstParityContract(t *testing.T) {
	root := filepath.Join("..", "..")
	appContract := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileAppContract.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))

	for _, want := range []string{
		"export type MobilePlatform",
		"android",
		"ios",
		"export type MobileViewID",
		"overview",
		"folders",
		"peers-identity",
		"transfers",
		"warnings-logs",
		"maintenance-backups",
		"daemon-settings",
		"mobile-app-settings",
		"help-details",
		"localEncryptedAPIOnly: true",
		"remoteManagementParity: true",
		"identityPairingImportExport: true",
		"animatedPairingScanner: true",
		"secureCredentialStoreRequired: true",
		"cellularSyncDisableSetting: true",
		"degradedStatusRequired: true",
		"buildMobileNavigationModel",
	} {
		if !strings.Contains(appContract, want) {
			t.Fatalf("mobile GUI app contract missing %q:\n%s", want, appContract)
		}
	}
	for _, want := range []string{
		"small-screen-first scaffold",
		"Android and iOS",
		"local encrypted API",
		"remote instance management",
		"identity pairing import/export",
		"animated pairing scanner",
		"platform secure credential storage",
		"cellular sync disable setting",
		"degraded sync status",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("mobile GUI README missing %q:\n%s", want, readme)
		}
	}
	for _, forbidden := range []string{
		"raw API keys in AsyncStorage",
		"raw API keys in UserDefaults",
		"daemon internals imported into UI",
		"private iOS API",
	} {
		if strings.Contains(appContract, forbidden) || strings.Contains(readme, forbidden) {
			t.Fatalf("mobile GUI scaffold contains forbidden platform/secret coupling claim %q", forbidden)
		}
	}
}

func TestMobileGUIIdentityPairingImportExportContract(t *testing.T) {
	root := filepath.Join("..", "..")
	appContract := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileAppContract.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))

	for _, want := range []string{
		"export type MobileIdentityPackageExportPresentation",
		"copyableText",
		"downloadableIdentityFile",
		"shareSheetPayload",
		"qrFallbackPayload",
		"animatedPairingFrames",
		"export type MobileIdentityPackageImportSource",
		"pastedText",
		"uploadedIdentityFile",
		"scannedAnimatedCode",
		"sharedIdentityFile",
		"export type MobilePairingSecureStorageTarget",
		"android-keystore",
		"ios-keychain",
		"buildMobileIdentityPairingActions",
		"parseMobileIdentityImportReadiness",
		"secureLocalStorageAfterImport",
	} {
		if !strings.Contains(appContract, want) {
			t.Fatalf("mobile GUI identity pairing contract missing %q:\n%s", want, appContract)
		}
	}
	for _, want := range []string{
		"copyable text",
		"shareable identity file",
		"pasted pairing code",
		"uploaded identity file",
		"animated pairing scanner",
		"Android Keystore",
		"iOS Keychain",
		"secure local storage after successful import",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("mobile GUI README missing identity pairing note %q:\n%s", want, readme)
		}
	}
	for _, forbidden := range []string{
		"daemon API key",
		"private identity key",
		"raw bootstrap proof in logs",
		"unencrypted local storage",
	} {
		if strings.Contains(appContract, forbidden) || strings.Contains(readme, forbidden) {
			t.Fatalf("mobile GUI identity pairing contract contains forbidden secret/storage claim %q", forbidden)
		}
	}
}

func TestMobileGUIAndroidShellDefinesSmallScreenNavigationAndPlatformBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type AndroidMobileShellState",
		"export type AndroidBackgroundCapabilityStatus",
		"buildAndroidMobileShellState",
		"platform: 'android'",
		"buildMobileNavigationModel",
		"selectedHostID",
		"activeView",
		"localEncryptedAPIOnly: true",
		"foregroundServiceRequired",
		"workManagerDeferredSyncRequired",
		"batteryOptimizationExemptionPrompt",
		"notificationPermissionRequired",
		"secureCredentialStore: 'android-keystore'",
		"cellularSyncDisabled",
		"degradedStatusReasons",
		"overview",
		"folders",
		"peers-identity",
		"transfers",
		"warnings-logs",
		"maintenance-backups",
		"daemon-settings",
		"mobile-app-settings",
		"help-details",
	} {
		if !strings.Contains(androidShell, want) {
			t.Fatalf("Android mobile shell contract missing %q:\n%s", want, androidShell)
		}
	}
	for _, want := range []string{
		"Android shell contract",
		"small-screen navigation",
		"foreground service",
		"WorkManager",
		"Android Keystore",
		"battery optimization exemption",
		"cellular sync disable",
		"degraded background-sync status",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("mobile GUI README missing Android shell note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"Latest Android shell checkpoint",
		"small-screen navigation shell",
		"Android foreground service",
		"WorkManager deferred sync",
		"Android Keystore",
		"degraded background-sync status",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("mobile GUI architecture doc missing Android shell progress note %q:\n%s", want, doc)
		}
	}
	for _, forbidden := range []string{
		"raw API key",
		"AsyncStorage",
		"private Android API",
		"imports daemon internals",
		"always-on background sync guaranteed",
	} {
		if strings.Contains(androidShell, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("Android mobile shell contract contains forbidden platform/secret claim %q", forbidden)
		}
	}
}

func TestMobileGUIIOShellDefinesSmallScreenNavigationAndPlatformBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type IOSMobileShellState",
		"export type IOSBackgroundCapabilityStatus",
		"buildIOSMobileShellState",
		"platform: 'ios'",
		"buildMobileNavigationModel",
		"selectedHostID",
		"activeView",
		"localEncryptedAPIOnly: true",
		"backgroundTasksRequired",
		"backgroundURLSessionRequired",
		"silentPushWakeSupported",
		"shortWakeWindowCheckpointing",
		"secureCredentialStore: 'ios-keychain'",
		"cellularSyncDisabled",
		"degradedStatusReasons",
		"overview",
		"folders",
		"peers-identity",
		"transfers",
		"warnings-logs",
		"maintenance-backups",
		"daemon-settings",
		"mobile-app-settings",
		"help-details",
	} {
		if !strings.Contains(iosShell, want) {
			t.Fatalf("iOS mobile shell contract missing %q:\n%s", want, iosShell)
		}
	}
	for _, want := range []string{
		"iOS shell contract",
		"small-screen navigation",
		"iOS background tasks",
		"background URLSession",
		"iOS Keychain",
		"short wake windows",
		"cellular sync disable",
		"degraded background-sync status",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("mobile GUI README missing iOS shell note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"Latest iOS shell checkpoint",
		"small-screen navigation shell",
		"iOS background tasks",
		"background URLSession",
		"iOS Keychain",
		"short wake-window checkpointing",
		"degraded background-sync status",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("mobile GUI architecture doc missing iOS shell progress note %q:\n%s", want, doc)
		}
	}
	for _, forbidden := range []string{
		"raw API key",
		"UserDefaults",
		"private iOS API",
		"imports daemon internals",
		"always-on background sync guaranteed",
	} {
		if strings.Contains(iosShell, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("iOS mobile shell contract contains forbidden platform/secret claim %q", forbidden)
		}
	}
}

func TestMobileGUIArchitectureDocumentsPlatformPolicyAndBackgroundSync(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"Android",
		"iOS",
		"bundled daemon",
		"local encrypted API",
		"Android foreground service",
		"WorkManager",
		"battery optimization exemption",
		"iOS background tasks",
		"background URLSession",
		"silent push",
		"short wake windows",
		"durable checkpoints",
		"degraded sync status",
		"network policy",
		"cellular",
		"secure identity/key storage",
		"identity pairing import/export",
		"animated pairing code scanning",
		"remote instance management",
		"identity mesh relay",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("mobile GUI architecture doc missing %q:\n%s", want, doc)
		}
	}
	for _, forbidden := range []string{
		"private API",
		"guaranteed continuous iOS daemon",
		"raw API keys in app storage",
		"always-on background sync on iOS",
	} {
		if strings.Contains(doc, forbidden) {
			t.Fatalf("mobile GUI architecture doc contains forbidden platform/secret claim %q:\n%s", forbidden, doc)
		}
	}
}

func TestMobileGUIAnimatedPairingScannerShowsProgressAndStoresSecurely(t *testing.T) {
	root := filepath.Join("..", "..")
	scanner := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileAnimatedPairingScanner.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type MobileAnimatedPairingFrame",
		"export type MobileAnimatedPairingScannerState",
		"export type MobileAnimatedPairingImportResult",
		"createMobileAnimatedPairingScannerState",
		"addMobileAnimatedPairingFrame",
		"completeMobileAnimatedPairingImport",
		"sessionId",
		"frameIndex",
		"frameCount",
		"payloadSha256",
		"payloadByteLength",
		"fragmentBase64",
		"collectedFrameCount",
		"totalFrameCount",
		"duplicateFrameCount",
		"missingFrameIndexes",
		"Keep phone pointed at screen until pairing is complete.",
		"complete-payload verification",
		"secureLocalStorageTarget",
		"android-keystore",
		"ios-keychain",
	} {
		if !strings.Contains(scanner, want) {
			t.Fatalf("mobile animated pairing scanner contract missing %q:\n%s", want, scanner)
		}
	}
	for _, want := range []string{
		"mobile animated pairing scanner checkpoint",
		"frame de-duplication/reordering",
		"collected frame count versus total frame count",
		"Keep phone pointed at screen until pairing is complete.",
		"secure local storage after verified import",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing animated scanner progress note %q", want)
		}
	}
	for _, forbidden := range []string{
		"raw API key",
		"localStorage",
		"AsyncStorage",
		"UserDefaults",
		"logs pairing payload",
	} {
		if strings.Contains(scanner, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("mobile animated scanner contract contains forbidden secret/storage claim %q", forbidden)
		}
	}
	if strings.Contains(scanner, "private identity key") {
		t.Fatalf("mobile animated scanner contract must not carry private identity keys")
	}
}

func TestMobileGUIAndroidBackgroundSyncPlannerUsesForegroundServiceWorkManagerAndNetworkPolicy(t *testing.T) {
	root := filepath.Join("..", "..")
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type AndroidBackgroundSyncWorkKind",
		"export type AndroidBackgroundSyncDecision",
		"buildAndroidBackgroundSyncDecision",
		"foreground-service",
		"workmanager-deferred",
		"blocked-by-cellular-policy",
		"blocked-by-permission",
		"requiresUserVisibleNotification",
		"requiresBatteryOptimizationExemptionPrompt",
		"metadata-catchup",
		"small-pending-transfer",
		"scrub-repair-check",
		"networkType",
		"metered",
		"chargingRequired",
		"durableCheckpointRequired",
	} {
		if !strings.Contains(androidShell, want) {
			t.Fatalf("Android background sync planner contract missing %q:\n%s", want, androidShell)
		}
	}
	for _, want := range []string{
		"Android background sync scheduler checkpoint",
		"foreground service for active sync",
		"WorkManager for deferred metadata catch-up, small pending transfers, scrub/repair checks, and retry work",
		"cellular sync disable policy blocks mobile network work before scheduling",
		"durable checkpoints before background work yields",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing Android background sync scheduler note %q", want)
		}
	}
	for _, forbidden := range []string{
		"private Android API",
		"raw API key",
		"always-on background sync guaranteed",
		"ignores cellular setting",
	} {
		if strings.Contains(androidShell, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("Android background sync contract contains forbidden platform/secret claim %q", forbidden)
		}
	}
}

func TestMobileGUIAndroidBatteryOptimizationExemptionUXIsExplicit(t *testing.T) {
	root := filepath.Join("..", "..")
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type AndroidBatteryOptimizationExemptionState",
		"buildAndroidBatteryOptimizationExemptionPrompt",
		"continuous-background-sync",
		"request-action-manage-ignore-battery-optimizations",
		"user-declined",
		"os-refused",
		"Battery optimization exemption is recommended only when continuous/background sync is enabled.",
		"Explain that Android may pause sync while the app is idle unless battery optimization is exempted.",
	} {
		if !strings.Contains(androidShell, want) {
			t.Fatalf("Android battery optimization UX contract missing %q:\n%s", want, androidShell)
		}
	}
	for _, want := range []string{
		"Android battery optimization exemption checkpoint",
		"request battery optimization exemption only when continuous/background sync is enabled",
		"explain that Android may pause sync while the app is idle",
		"surface degraded status if the OS refuses or the user declines",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing Android battery optimization note %q", want)
		}
	}
	for _, forbidden := range []string{
		"force battery optimization exemption",
		"bypass Android battery optimization",
		"ignore user declined battery optimization",
	} {
		if strings.Contains(androidShell, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("Android battery optimization contract contains forbidden claim %q", forbidden)
		}
	}
}

func TestMobileGUIIOSBackgroundSyncPlannerUsesApprovedWakeWindowsAndCheckpoints(t *testing.T) {
	root := filepath.Join("..", "..")
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type IOSBackgroundSyncWorkKind",
		"export type IOSBackgroundSyncDecision",
		"export type IOSShortWakeWindowPlan",
		"buildIOSBackgroundSyncDecision",
		"buildIOSShortWakeWindowPlan",
		"background-app-refresh",
		"background-task",
		"background-url-session",
		"silent-push-wake",
		"foreground-only",
		"blocked-by-cellular-policy",
		"blocked-by-platform-policy",
		"metadata-catchup",
		"small-pending-transfer",
		"resumable-block-chunk",
		"checkpoint-flush",
		"retry-after-connectivity",
		"remainingWakeBudgetSeconds",
		"exitDeadlineSeconds",
		"durableCheckpointBeforeExit",
		"rescheduleNextWakeBeforeExit",
		"rescheduleBeforeSuspension",
		"durableCheckpointRequired",
	} {
		if !strings.Contains(iosShell, want) {
			t.Fatalf("iOS background sync planner contract missing %q:\n%s", want, iosShell)
		}
	}
	for _, want := range []string{
		"iOS background sync scheduler checkpoint",
		"documented wake/work opportunities",
		"background app refresh",
		"background tasks",
		"background URLSession",
		"silent push wakeups",
		"Short wake windows prioritize metadata catch-up, small pending transfers, resumable block chunks, and durable checkpoint flushes",
		"reschedule the next permitted wake before suspension",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing iOS background sync scheduler note %q", want)
		}
	}
	for _, forbidden := range []string{
		"private iOS API",
		"raw API key",
		"always-on background sync guaranteed",
		"ignores cellular setting",
	} {
		if strings.Contains(iosShell, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("iOS background sync contract contains forbidden platform/secret claim %q", forbidden)
		}
	}
}

func TestMobileGUIAndroidCameraCaptureFeedsAnimatedPairingScanner(t *testing.T) {
	root := filepath.Join("..", "..")
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type AndroidAnimatedPairingCameraPermissionState",
		"export interface AndroidAnimatedPairingCameraCaptureState",
		"buildAndroidAnimatedPairingCameraCaptureState",
		"handleAndroidAnimatedPairingCameraFrame",
		"cameraPermission",
		"cameraPreviewActive",
		"decodedFrameCount",
		"rejectedFrameCount",
		"scannerState",
		"Keep phone pointed at screen until pairing is complete.",
		"complete-payload verification",
		"android-keystore",
	} {
		if !strings.Contains(androidShell, want) {
			t.Fatalf("Android animated pairing camera capture contract missing %q:\n%s", want, androidShell)
		}
	}
	for _, want := range []string{
		"Android animated pairing camera checkpoint",
		"camera permission",
		"camera preview",
		"decoded animated frames into the shared scanner state",
		"Keep phone pointed at screen until pairing is complete.",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing Android camera scanner note %q", want)
		}
	}
	for _, forbidden := range []string{"raw API key", "localStorage", "AsyncStorage", "private identity key", "logs pairing payload"} {
		if strings.Contains(androidShell, forbidden) {
			t.Fatalf("Android camera scanner contract contains forbidden secret/storage claim %q", forbidden)
		}
	}
}

func TestMobileGUIIOSCameraCaptureFeedsAnimatedPairingScanner(t *testing.T) {
	root := filepath.Join("..", "..")
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type IOSAnimatedPairingCameraAuthorizationState",
		"export interface IOSAnimatedPairingCameraCaptureState",
		"buildIOSAnimatedPairingCameraCaptureState",
		"handleIOSAnimatedPairingCameraFrame",
		"cameraAuthorization",
		"cameraSessionActive",
		"decodedFrameCount",
		"rejectedFrameCount",
		"scannerState",
		"Keep phone pointed at screen until pairing is complete.",
		"complete-payload verification",
		"ios-keychain",
	} {
		if !strings.Contains(iosShell, want) {
			t.Fatalf("iOS animated pairing camera capture contract missing %q:\n%s", want, iosShell)
		}
	}
	for _, want := range []string{
		"iOS animated pairing camera checkpoint",
		"camera authorization",
		"AVFoundation capture session",
		"decoded animated frames into the shared scanner state",
		"Keep phone pointed at screen until pairing is complete.",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing iOS camera scanner note %q", want)
		}
	}
	for _, forbidden := range []string{"raw API key", "localStorage", "UserDefaults", "private identity key", "logs pairing payload"} {
		if strings.Contains(iosShell, forbidden) {
			t.Fatalf("iOS camera scanner contract contains forbidden secret/storage claim %q", forbidden)
		}
	}
}

func TestMobileGUIAnimatedPairingScannerRendersProgressAndCompletionUI(t *testing.T) {
	root := filepath.Join("..", "..")
	screen := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileAnimatedPairingScannerScreen.ts"))
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type MobileAnimatedPairingScannerScreenModel",
		"buildMobileAnimatedPairingScannerScreen",
		"progressPercent",
		"progressText",
		"collectedFrameCount",
		"requiredFrameCount",
		"totalFrameCount",
		"missingFrameIndexes",
		"Keep phone pointed at screen until pairing is complete.",
		"Pairing is complete",
		"connectivity and authorization will continue in the background",
		"secureLocalStorageTarget",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("mobile animated scanner screen contract missing %q:\n%s", want, screen)
		}
	}
	for _, want := range []string{
		"scannerScreen",
		"buildMobileAnimatedPairingScannerScreen",
	} {
		if !strings.Contains(androidShell, want) || !strings.Contains(iosShell, want) {
			t.Fatalf("mobile platform shell missing scanner screen binding %q", want)
		}
	}
	for _, want := range []string{
		"mobile animated pairing scanner UI checkpoint",
		"collected/required/total frame progress",
		"Pairing is complete",
		"continues silently in the background",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing animated scanner UI note %q", want)
		}
	}
	for _, forbidden := range []string{"raw API key", "localStorage", "AsyncStorage", "UserDefaults", "logs pairing payload", "private identity key"} {
		if strings.Contains(screen, forbidden) {
			t.Fatalf("mobile animated scanner screen contains forbidden secret/storage claim %q", forbidden)
		}
	}
}

func TestMobileGUISurfacesDegradedSyncStatusForAndroidAndIOS(t *testing.T) {
	root := filepath.Join("..", "..")
	statusModel := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileDegradedStatus.ts"))
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type MobileDegradedSyncCause",
		"ios-policy",
		"battery-state",
		"user-settings",
		"notification-permission",
		"network-state",
		"background-refresh",
		"continuous daemon-style syncing is degraded",
		"buildMobileDegradedSyncStatus",
		"visible: reasons.length > 0",
		"actionRequired",
	} {
		if !strings.Contains(statusModel, want) {
			t.Fatalf("mobile degraded sync status model missing %q:\n%s", want, statusModel)
		}
	}
	for _, want := range []string{
		"buildMobileDegradedSyncStatus",
		"degradedSyncStatus",
		"notification-permission",
		"battery-state",
		"user-settings",
		"network-state",
	} {
		if !strings.Contains(androidShell, want) {
			t.Fatalf("Android shell missing degraded sync status binding %q:\n%s", want, androidShell)
		}
	}
	for _, want := range []string{
		"buildMobileDegradedSyncStatus",
		"degradedSyncStatus",
		"ios-policy",
		"background-refresh",
		"notification-permission",
		"battery-state",
		"user-settings",
		"network-state",
	} {
		if !strings.Contains(iosShell, want) {
			t.Fatalf("iOS shell missing degraded sync status binding %q:\n%s", want, iosShell)
		}
	}
	for _, want := range []string{
		"mobile degraded sync status checkpoint",
		"iOS policy",
		"battery state",
		"user settings",
		"notification permission",
		"network state",
		"background refresh restrictions",
		"continuous daemon-style syncing is degraded",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing degraded sync status note %q", want)
		}
	}
	for _, forbidden := range []string{"raw API key", "private iOS API", "private Android API", "always-on background sync guaranteed"} {
		if strings.Contains(statusModel, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("mobile degraded sync status contract contains forbidden claim %q", forbidden)
		}
	}
}

func TestMobileGUIPlansPlatformPermissionsFromConfiguredSyncOptions(t *testing.T) {
	root := filepath.Join("..", "..")
	permissions := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobilePermissions.ts"))
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type MobilePlatformPermissionID",
		"android-notifications",
		"android-manage-external-storage",
		"android-nearby-wifi-devices",
		"android-location-for-wifi-ssid",
		"android-foreground-service-data-sync",
		"android-ignore-battery-optimizations",
		"ios-user-notifications",
		"ios-local-network",
		"ios-background-app-refresh",
		"ios-background-processing",
		"ios-location-when-in-use-for-network-state",
		"buildMobilePermissionPlan",
		"requireLocationForWifiNetworkState",
		"continuousBackgroundSyncEnabled",
		"reason",
		"userFacingExplanation",
	} {
		if !strings.Contains(permissions, want) {
			t.Fatalf("mobile permission planner missing %q:\n%s", want, permissions)
		}
	}
	for _, want := range []string{
		"buildMobilePermissionPlan",
		"permissionPlan",
		"android-location-for-wifi-ssid",
		"android-ignore-battery-optimizations",
	} {
		if !strings.Contains(androidShell, want) {
			t.Fatalf("Android shell missing permission planner binding %q:\n%s", want, androidShell)
		}
	}
	for _, want := range []string{
		"buildMobilePermissionPlan",
		"permissionPlan",
		"ios-location-when-in-use-for-network-state",
		"ios-background-processing",
	} {
		if !strings.Contains(iosShell, want) {
			t.Fatalf("iOS shell missing permission planner binding %q:\n%s", want, iosShell)
		}
	}
	for _, want := range []string{
		"mobile permissions checkpoint",
		"storage/network/location/notification/background permissions",
		"based on configured sync options",
		"location permission when required to detect Wi-Fi/network state",
		"permission planner is a request/status contract, not a native entitlement implementation",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing permission-planner note %q", want)
		}
	}
	for _, forbidden := range []string{"private Android API", "private iOS API", "request every permission", "raw API key"} {
		if strings.Contains(permissions, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("mobile permission planner contract contains forbidden claim %q", forbidden)
		}
	}
}

func TestMobileGUIEnforcesCellularSyncDisableSetting(t *testing.T) {
	root := filepath.Join("..", "..")
	appSettings := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileSyncSettings.ts"))
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export interface MobileSyncNetworkPolicy",
		"cellularSyncDisabled",
		"buildMobileSyncNetworkPolicy",
		"shouldBlockMobileSyncForNetwork",
		"wifi",
		"ethernet",
		"cellular",
		"blocked-by-cellular-policy",
		"Mobile cellular sync is disabled by user settings.",
	} {
		if !strings.Contains(appSettings, want) {
			t.Fatalf("mobile cellular sync setting contract missing %q:\n%s", want, appSettings)
		}
	}
	for _, want := range []string{
		"buildMobileSyncNetworkPolicy",
		"shouldBlockMobileSyncForNetwork",
		"cellularSyncDisabled",
		"blocked-by-cellular-policy",
	} {
		if !strings.Contains(androidShell, want) {
			t.Fatalf("Android shell missing cellular setting enforcement %q:\n%s", want, androidShell)
		}
		if !strings.Contains(iosShell, want) {
			t.Fatalf("iOS shell missing cellular setting enforcement %q:\n%s", want, iosShell)
		}
	}
	for _, want := range []string{
		"mobile cellular sync setting checkpoint",
		"disable synchronization on mobile/cellular networks",
		"Android and iOS network capability/state checks",
		"blocked before scheduling",
		"Wi-Fi and Ethernet remain allowed",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing cellular sync setting note %q", want)
		}
	}
	for _, forbidden := range []string{"ignore cellular setting", "always sync on cellular", "raw API key"} {
		if strings.Contains(appSettings, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("mobile cellular sync setting contract contains forbidden claim %q", forbidden)
		}
	}
}

func TestMobileGUIWiresDaemonAPIClientForOperationalScreens(t *testing.T) {
	root := filepath.Join("..", "..")
	apiClient := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileDaemonApi.ts"))
	appContract := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileAppContract.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type MobileDaemonConnectionSettings",
		"credentialRef",
		"resolveCredential",
		"X-API-Key",
		"fetchMobileDaemonStatus",
		"fetchMobileDaemonFolders",
		"sendMobilePeerCommand",
		"sendMobileFolderCommand",
		"sendMobileDiscoveryCommand",
		"sendMobileTransferCommand",
		"fetchMobileRecentLogs",
		"runMobileMaintenanceScrub",
		"fetchMobileBackupJobs",
		"readMobileDaemonConfig",
		"patchMobileDaemonConfig",
		"/v1/status",
		"/v1/folders",
		"/v1/peers",
		"/v1/discovery-command",
		"/v1/transfer-command",
		"/v1/logs",
		"/v1/maintenance/scrub",
		"/v1/backup/jobs",
	} {
		if !strings.Contains(apiClient, want) {
			t.Fatalf("mobile daemon API client missing %q:\n%s", want, apiClient)
		}
	}
	for _, want := range []string{
		"mobileOperationalAPIClient: true",
		"folders, peers, discovery, transfers, warnings/logs, maintenance, backups, and settings",
	} {
		if !strings.Contains(appContract, want) {
			t.Fatalf("mobile app contract missing operational API wiring note %q:\n%s", want, appContract)
		}
	}
	for _, want := range []string{
		"mobile daemon API client",
		"folders, peers, discovery, transfers, warnings/logs, maintenance, backups, and settings",
		"credential references only",
		"platform secure storage resolves API keys at call time",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile GUI docs missing daemon API client note %q", want)
		}
	}
	screens := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileScreens.ts"))
	for _, want := range []string{
		"buildMobileOperationalScreenBindings",
		"fetchMobileDaemonStatus",
		"fetchMobileDaemonFolders",
		"fetchMobileDaemonPeers",
		"sendMobilePeerCommand",
		"sendMobileFolderCommand",
		"sendMobileDiscoveryCommand",
		"sendMobileTransferCommand",
		"fetchMobileRecentLogs",
		"runMobileMaintenanceScrub",
		"fetchMobileBackupJobs",
		"readMobileDaemonConfig",
		"patchMobileDaemonConfig",
		"overview",
		"folders",
		"peers-identity",
		"transfers",
		"warnings-logs",
		"maintenance-backups",
		"daemon-settings",
	} {
		if !strings.Contains(screens, want) {
			t.Fatalf("mobile operational screen binding missing %q:\n%s", want, screens)
		}
	}
	for _, forbidden := range []string{"apiKey: string", "localStorage", "AsyncStorage", "UserDefaults", "child_process", "exec(", "spawn(", "internal/daemon"} {
		if strings.Contains(apiClient, forbidden) || strings.Contains(appContract, forbidden) || strings.Contains(screens, forbidden) {
			t.Fatalf("mobile daemon API client must keep API-only/secret-store boundaries, found %q", forbidden)
		}
	}
}

func TestMobileGUIRemoteInstanceOnboardingAndIdentityDiscoveryContract(t *testing.T) {
	root := filepath.Join("..", "..")
	remote := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileRemoteInstances.ts"))
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type MobileRemoteInstanceOnboardingSource",
		"direct-api-endpoint-key",
		"pasted-pairing-code",
		"scanned-animated-pairing-code",
		"shared-identity-file",
		"uploaded-identity-file",
		"credentialRef",
		"secureStorageTarget",
		"android-keystore",
		"ios-keychain",
		"buildMobileRemoteInstanceCandidate",
		"autoPopulateMobileSameIdentityInstances",
		"peerIDCode",
		"reachable",
		"progressiveStatusHydration",
		"raw API keys must be stored only through the platform secure credential store",
	} {
		if !strings.Contains(remote, want) {
			t.Fatalf("mobile remote-instance contract missing %q:\n%s", want, remote)
		}
	}
	for _, want := range []string{
		"mobileRemoteInstanceOnboardingSources",
		"direct-api-endpoint-key",
		"pasted-pairing-code",
		"scanned-animated-pairing-code",
		"shared-identity-file",
		"autoPopulateMobileSameIdentityInstances",
	} {
		if !strings.Contains(androidShell, want) || !strings.Contains(iosShell, want) {
			t.Fatalf("mobile platform shell missing remote instance onboarding hook %q", want)
		}
	}
	for _, want := range []string{
		"remote instance onboarding",
		"direct API endpoint/key",
		"pasted pairing code",
		"scanned animated pairing code",
		"shared identity file",
		"Android Keystore",
		"iOS Keychain",
		"automatic same-identity instance population",
		"progressively hydrates status",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile remote-instance docs missing %q", want)
		}
	}
	for _, forbidden := range []string{"apiKey: string", "rawAPIKey", "AsyncStorage", "UserDefaults", "localStorage", "child_process", "exec(", "spawn(", "internal/daemon"} {
		if strings.Contains(remote, forbidden) || strings.Contains(androidShell, forbidden) || strings.Contains(iosShell, forbidden) {
			t.Fatalf("mobile remote-instance contract violates secret/API-only boundary with %q", forbidden)
		}
	}
}

func TestMobileGUISupportsIdentityMeshRelayForRemoteManagement(t *testing.T) {
	root := filepath.Join("..", "..")
	mesh := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileMeshRelay.ts"))
	apiClient := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileDaemonApi.ts"))
	screens := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "app", "mobileScreens.ts"))
	androidShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "android", "androidShell.ts"))
	iosShell := readRequiredFile(t, filepath.Join(root, "mobile-gui", "src", "ios", "iosShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "mobile-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "MOBILE_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type MobileMeshRelayRoute",
		"direct-encrypted-api",
		"identity-mesh-relay",
		"offline-queued",
		"buildMobileMeshRelayRoute",
		"buildMobileOfflineSettingsEdit",
		"durablePendingChangeRequired: true",
		"eventuallyConsistentDelivery: true",
		"authenticationRequired: true",
		"authorizationRequired: true",
		"acknowledgementRequired: true",
		"pending/applied/failed/acknowledged",
	} {
		if !strings.Contains(mesh, want) {
			t.Fatalf("mobile mesh relay contract missing %q:\n%s", want, mesh)
		}
	}
	for _, want := range []string{
		"fetchMobileMeshSettings",
		"sendMobileMeshSettingsCommand",
		"/v1/mesh/settings",
		"/v1/mesh/settings-command",
	} {
		if !strings.Contains(apiClient, want) || !strings.Contains(screens, want) {
			t.Fatalf("mobile mesh relay API/screen binding missing %q", want)
		}
	}
	for _, want := range []string{
		"buildMobileMeshRelayRoute",
		"mobileMeshRelayStatus",
		"offline-queued",
		"identity-mesh-relay",
	} {
		if !strings.Contains(androidShell, want) || !strings.Contains(iosShell, want) {
			t.Fatalf("mobile platform shell missing mesh relay binding %q", want)
		}
	}
	for _, want := range []string{
		"mobile identity mesh relay checkpoint",
		"identity-linked mesh relay",
		"unreachable instances can be inspected or configured through reachable identity peers",
		"durable pending settings changes",
		"pending/applied/failed/acknowledged",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("mobile docs missing mesh relay note %q", want)
		}
	}
	for _, forbidden := range []string{"raw API key", "apiKey: string", "AsyncStorage", "UserDefaults", "localStorage", "child_process", "exec(", "spawn(", "internal/daemon"} {
		if strings.Contains(mesh, forbidden) || strings.Contains(apiClient, forbidden) || strings.Contains(screens, forbidden) || strings.Contains(androidShell, forbidden) || strings.Contains(iosShell, forbidden) {
			t.Fatalf("mobile mesh relay contract violates API-only/secret boundary with %q", forbidden)
		}
	}
}

func TestDesktopGUIArchitectureChoosesDecoupledSixTargetStack(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))
	for _, want := range []string{
		"Wails + Svelte/TypeScript",
		"separate daemon process",
		"Windows amd64",
		"Windows arm64",
		"macOS amd64",
		"macOS arm64",
		"Linux amd64",
		"Linux arm64",
		"tray",
		"startup/login/start-at-boot",
		"encrypted API",
		"bundled engine binary",
		"reproducible packaging",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing %q:\n%s", want, doc)
		}
	}
	for _, forbidden := range []string{
		"daemon runs inside the GUI process",
		"GUI must stay open for sync",
		"manual config editing is required",
	} {
		if strings.Contains(doc, forbidden) {
			t.Fatalf("desktop GUI architecture doc contains forbidden coupling claim %q:\n%s", forbidden, doc)
		}
	}
}

func TestDesktopGUIBundleContractVerifiesEngineAndUsesEncryptedAPI(t *testing.T) {
	root := filepath.Join("..", "..")
	bundleContract := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "bundledEngine.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))

	for _, want := range []string{
		"export type DesktopTarget",
		"windows-amd64",
		"windows-arm64",
		"darwin-amd64",
		"darwin-arm64",
		"linux-amd64",
		"linux-arm64",
		"expectedExecutable",
		"expectedVersion",
		"expectedSHA256",
		"verifyBundledEngine",
		"controlBundledDaemonLifecycle",
		"/v1/service-command",
		"X-API-Key",
		"https://",
	} {
		if !strings.Contains(bundleContract, want) {
			t.Fatalf("desktop GUI bundled-engine contract missing %q:\n%s", want, bundleContract)
		}
	}
	for _, want := range []string{
		"bundle manifest maps each OS/architecture to exactly one daemon executable",
		"verifies the bundled executable name, version, and SHA-256 metadata before offering lifecycle controls",
		"Lifecycle control is API-only through the authenticated encrypted daemon API",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing bundled-engine lifecycle contract %q:\n%s", want, readme)
		}
	}
	for _, forbidden := range []string{
		"child_process",
		"exec(",
		"spawn(",
		"http://",
		"internal/daemon",
		"../cmd/fse",
	} {
		if strings.Contains(bundleContract, forbidden) {
			t.Fatalf("desktop GUI bundle contract must verify and control through encrypted API, found forbidden %q", forbidden)
		}
	}
}

func TestDesktopGUIEngineResourcePackagerCopiesSixReleaseBinaries(t *testing.T) {
	root := filepath.Join("..", "..")
	packager := readRequiredFile(t, filepath.Join(root, "scripts", "package-desktop-engine-resources.sh"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))

	for _, want := range []string{
		"desktop-gui/resources/engine",
		"fse-linux-amd64",
		"fse-linux-arm64",
		"fse-darwin-amd64",
		"fse-darwin-arm64",
		"fse-windows-amd64.exe",
		"fse-windows-arm64.exe",
		"linux/amd64/fse",
		"linux/arm64/fse",
		"darwin/amd64/fse",
		"darwin/arm64/fse",
		"windows/amd64/fse.exe",
		"windows/arm64/fse.exe",
		"SHA256SUMS",
		"manifest.json",
	} {
		if !strings.Contains(packager, want) {
			t.Fatalf("desktop engine resource packager missing %q:\n%s", want, packager)
		}
	}
	for _, forbidden := range []string{
		"go build",
		"npm install",
		"wails build",
	} {
		if strings.Contains(packager, forbidden) {
			t.Fatalf("desktop engine resource packager must copy existing release binaries without installing/building tooling, found %q", forbidden)
		}
	}
	for _, want := range []string{
		"scripts/package-desktop-engine-resources.sh <version>",
		"copies the six existing daemon release binaries into `desktop-gui/resources/engine/`",
		"does not build or install toolchains",
		"writes a resource `manifest.json` plus `SHA256SUMS`",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing resource packaging contract %q:\n%s", want, readme)
		}
	}
}

func TestDesktopGUIWailsNativeShellEntrypointExists(t *testing.T) {
	root := filepath.Join("..", "..")
	goMod := readRequiredFile(t, filepath.Join(root, "desktop-gui", "go.mod"))
	mainGo := readRequiredFile(t, filepath.Join(root, "desktop-gui", "main.go"))
	appGo := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app.go"))
	wailsConfig := readRequiredFile(t, filepath.Join(root, "desktop-gui", "wails.json"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))

	for _, want := range []string{
		"module filesyncengine/desktop-gui",
		"github.com/wailsapp/wails/v2",
	} {
		if !strings.Contains(goMod, want) {
			t.Fatalf("desktop GUI Wails Go module missing %q:\n%s", want, goMod)
		}
	}
	for _, want := range []string{
		"package main",
		"//go:embed all:dist",
		"wails.Run",
		"assetserver.Options",
		"OnStartup:",
		"app.startup",
		"Bind: []interface{}",
		"File Synchronization Engine Desktop",
	} {
		if !strings.Contains(mainGo, want) {
			t.Fatalf("desktop GUI Wails entrypoint missing %q:\n%s", want, mainGo)
		}
	}
	for _, want := range []string{
		"type App struct",
		"func NewApp() *App",
		"func (a *App) startup(ctx context.Context)",
		"func (a *App) RuntimeInfo() DesktopRuntimeInfo",
		"SeparateProcessDaemon: true",
		"ControlPlane:",
		"authenticated-encrypted-api",
	} {
		if !strings.Contains(appGo, want) {
			t.Fatalf("desktop GUI Wails app bridge missing %q:\n%s", want, appGo)
		}
	}
	for _, want := range []string{
		"frontend:dir",
		"frontend:build",
		"npm run build",
	} {
		if !strings.Contains(wailsConfig, want) {
			t.Fatalf("desktop GUI Wails config missing %q:\n%s", want, wailsConfig)
		}
	}
	for _, want := range []string{
		"The native Wails shell now has a real Go entrypoint",
		"desktop-gui/go.mod",
		"desktop-gui/main.go",
		"desktop-gui/app.go",
		"frontend `dist/` assets",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing Wails native shell note %q:\n%s", want, readme)
		}
	}
}

func TestDesktopGUIWailsBuilderImageDefinitionIsProjectOwnedAndIsolated(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "Dockerfile"))
	linuxArm64Dockerfile := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "Dockerfile.linux-arm64"))
	linuxArm64CrossDockerfile := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "Dockerfile.linux-arm64-cross"))
	builderScript := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-wails-builder-image.sh"))
	wailsBuildScript := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-gui-wails.sh"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"FROM golang:1.23-bookworm",
		"nodejs",
		"npm",
		"libwebkit2gtk-4.1-dev",
		"mingw-w64",
		"gcc-aarch64-linux-gnu",
		"github.com/wailsapp/wails/v2/cmd/wails@v2.10.2",
		"linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("desktop GUI Wails builder Dockerfile missing %q:\n%s", want, dockerfile)
		}
	}
	for _, want := range []string{
		"FROM --platform=linux/arm64 golang:1.23-bookworm",
		"libgtk-3-dev",
		"libwebkit2gtk-4.1-dev",
		"FSE_DESKTOP_WAILS_NATIVE_LINUX_ARM64=1",
		"github.com/wailsapp/wails/v2/cmd/wails@v2.10.2",
		"linux/arm64",
	} {
		if !strings.Contains(linuxArm64Dockerfile, want) {
			t.Fatalf("desktop GUI native linux-arm64 Wails builder Dockerfile missing %q:\n%s", want, linuxArm64Dockerfile)
		}
	}
	for _, want := range []string{
		"FROM fse-desktop-wails-builder:debian12-wails2.10.2",
		"dpkg --add-architecture arm64",
		"gcc-aarch64-linux-gnu",
		"g++-aarch64-linux-gnu",
		"libgtk-3-dev:arm64",
		"libwebkit2gtk-4.1-dev:arm64",
		"PKG_CONFIG_LIBDIR=/usr/lib/aarch64-linux-gnu/pkgconfig:/usr/share/pkgconfig",
		"aarch64-linux-gnu-gcc",
		"linux/arm64",
	} {
		if !strings.Contains(linuxArm64CrossDockerfile, want) {
			t.Fatalf("desktop GUI linux-arm64 cross Wails builder Dockerfile missing %q:\n%s", want, linuxArm64CrossDockerfile)
		}
	}
	for _, want := range []string{
		"docker build",
		"/development/fse-desktop-wails-builder-cache",
		"fse-desktop-wails-builder:debian12-wails2.10.2",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE",
		"FSE_DESKTOP_WAILS_BUILDER_DOCKERFILE",
		"FSE_DESKTOP_WAILS_BUILDER_PLATFORM",
		"DOCKER_BUILDKIT=1",
		"scripts/build-desktop-gui-wails.sh",
		"FSE_DESKTOP_SKIP_NETWORK_PREFLIGHT",
		"FSE_DESKTOP_CONTAINER_NETWORK",
		"getent hosts deb.debian.org",
		"\"$RUNTIME\" run --rm \"${NETWORK_ARGS[@]}\" debian:12",
		"\"$RUNTIME\" build \"${NETWORK_ARGS[@]}\" \"${PLATFORM_ARGS[@]}\"",
		"network preflight failed",
	} {
		if !strings.Contains(builderScript, want) {
			t.Fatalf("desktop GUI Wails builder image script missing %q:\n%s", want, builderScript)
		}
	}
	for _, want := range []string{
		"FSE_DESKTOP_CONTAINER_NETWORK",
		"NETWORK_ARGS=()",
		"RUN_RM_ARGS=(--rm)",
		"container_run \"${NETWORK_ARGS[@]}\"",
		"-e HOME=/tmp",
		"-e NPM_CONFIG_CACHE=/tmp/npm-cache",
		"-e GOCACHE=/tmp/go-cache",
		"-e GOPATH=/tmp/go",
		"-e GOMODCACHE=/tmp/go/pkg/mod",
		"COMPILER_ARGS=()",
		"image_inspect_architecture()",
		"host_architecture()",
		"FSE_DESKTOP_ALLOW_EMULATED_TARGET_RUN",
		"refusing to run target builder image through CPU emulation",
		"image inspect --format",
		"platform_run_args()",
		"platform_run_args \"$platform\" \"$image_arch\"",
		"RUN_PLATFORM_ARGS=()",
		"local image_arch=\"$2\"",
		"if [[ \"$platform\" = linux/arm64 && \"$image_arch\" = arm64 ]]",
		"--platform \"linux/arm64\"",
		"dpkg --print-architecture",
		"PKG_CONFIG_LIBDIR=/usr/lib/aarch64-linux-gnu/pkgconfig:/usr/share/pkgconfig",
		"windows/amd64) COMPILER_ARGS=(-e CC=x86_64-w64-mingw32-gcc -e CXX=x86_64-w64-mingw32-g++) ;;",
		"preflight_target_toolchain()",
		"command -v aarch64-linux-gnu-gcc",
		"pkg-config --exists gtk+-3.0 $linux_webkit_pkg",
		"FSE_DESKTOP_LINUX_WEBKIT_API",
		"FSE_DESKTOP_WAILS_TAGS_LINUX",
		"webkit2_41",
		"webkit2_40",
		"PKG_CONFIG_LIBDIR=/usr/lib/aarch64-linux-gnu/pkgconfig:/usr/share/pkgconfig",
		"command -v x86_64-w64-mingw32-gcc",
		"missing target toolchain for",
	} {
		if !strings.Contains(wailsBuildScript, want) {
			t.Fatalf("desktop GUI Wails build script missing isolated writable build environment support %q:\n%s", want, wailsBuildScript)
		}
	}
	packageJSON := readRequiredFile(t, filepath.Join(root, "desktop-gui", "package.json"))
	viteConfig := readRequiredFile(t, filepath.Join(root, "desktop-gui", "vite.config.ts"))
	for _, want := range []string{
		"svelte-preprocess",
	} {
		if !strings.Contains(packageJSON, want) {
			t.Fatalf("desktop GUI package missing Svelte TypeScript preprocessing dependency %q:\n%s", want, packageJSON)
		}
	}
	for _, want := range []string{
		"import preprocess from 'svelte-preprocess'",
		"svelte({ preprocess: preprocess() })",
	} {
		if !strings.Contains(viteConfig, want) {
			t.Fatalf("desktop GUI Vite config missing Svelte TypeScript preprocessing support %q:\n%s", want, viteConfig)
		}
	}
	for _, want := range []string{
		"development/desktop-wails-builder/Dockerfile",
		"scripts/build-desktop-wails-builder-image.sh",
		"fse-desktop-wails-builder:debian12-wails2.10.2",
		"development/desktop-wails-builder/Dockerfile.linux-arm64-cross",
		"fse-desktop-wails-builder:debian12-wails2.10.2-linux-arm64-cross",
		"development/desktop-wails-builder/Dockerfile.linux-webkit40",
		"development/desktop-wails-builder/Dockerfile.linux-arm64-cross-webkit40",
		"does not install build tooling on the Hermes host",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing Wails builder image note %q:\n%s", want, readme)
		}
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing Wails builder image note %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUIWindowsARM64WailsBuilderLayerUsesLLVMMinGW(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "Dockerfile.windows-arm64-llvm-mingw"))
	wailsBuildScript := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-gui-wails.sh"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"FROM fse-desktop-wails-builder:debian12-wails2.10.2",
		"LLVM_MINGW_VERSION=20250114",
		"llvm-mingw-20250114-ucrt-ubuntu-20.04-x86_64.tar.xz",
		"/opt/llvm-mingw/bin/aarch64-w64-mingw32-gcc",
		"/usr/local/bin/llvm-mingw",
		"windows/arm64",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("desktop GUI windows-arm64 Wails builder Dockerfile missing %q:\n%s", want, dockerfile)
		}
	}
	for _, want := range []string{
		"windows/arm64) check=\"$check && command -v llvm-mingw >/dev/null && command -v aarch64-w64-mingw32-gcc >/dev/null && command -v aarch64-w64-mingw32-g++ >/dev/null\" ;;",
		"windows/arm64) COMPILER_ARGS=(-e CC=aarch64-w64-mingw32-gcc -e CXX=aarch64-w64-mingw32-g++) ;;",
	} {
		if !strings.Contains(wailsBuildScript, want) {
			t.Fatalf("desktop GUI Wails build script missing windows-arm64 LLVM-MinGW wiring %q:\n%s", want, wailsBuildScript)
		}
	}
	for _, want := range []string{
		"Dockerfile.windows-arm64-llvm-mingw",
		"fse-desktop-wails-builder:debian12-wails2.10.2-windows-arm64-llvm-mingw",
		"Windows ARM64 LLVM-MinGW builder layer",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing windows-arm64 builder layer note %q:\n%s", want, readme)
		}
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing windows-arm64 builder layer note %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUIDarwinWailsBuilderLayerRequiresExternalSDKAndOsxcross(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "Dockerfile.darwin-osxcross"))
	patchScript := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "patch-osxcross-sdk.py"))
	builderScript := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-wails-darwin-builder-image.sh"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"FROM fse-desktop-wails-builder:debian12-wails2.10.2",
		"tpoechtrager/osxcross",
		"MacOSX*.sdk.tar*",
		"OSXCROSS_NO_INCLUDE_PATH_WARNINGS=1",
		"COPY MacOSX*.sdk.tar* /opt/osxcross/tarballs/",
		"COPY patch-osxcross-sdk.py /opt/osxcross/patch-osxcross-sdk.py",
		"RUN python3 /opt/osxcross/patch-osxcross-sdk.py",
		"o64-clang",
		"oa64-clang",
		"darwin/amd64 darwin/arm64",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("desktop GUI darwin Wails builder Dockerfile missing %q:\n%s", want, dockerfile)
		}
	}
	for _, forbidden := range []string{
		"<<'PY'",
		"<<PY",
		"python3 -c",
	} {
		if strings.Contains(dockerfile, forbidden) {
			t.Fatalf("desktop GUI darwin Wails builder Dockerfile must avoid inline Python shell quoting hazards for old Docker parser compatibility, found %q", forbidden)
		}
	}
	for _, want := range []string{
		"26.2*) TARGET=darwin25.2",
		"26.*) TARGET=darwin25",
		"expected osxcross SDK 26.2 case not found",
		"PATCHED_COMPILER_VALIDATION",
		"macOS 26 SDK libc++ headers",
		"test_compiler $ARCH-apple-$TARGET-clang++ $BASE_DIR/oclang/test.cpp \"\"",
	} {
		if !strings.Contains(patchScript, want) {
			t.Fatalf("desktop GUI darwin SDK patch script missing %q:\n%s", want, patchScript)
		}
	}
	for _, want := range []string{
		"Usage: scripts/build-desktop-wails-darwin-builder-image.sh <sdk-tarball> [image-tag]",
		"FSE_MACOS_SDK_TARBALL",
		"FSE_DESKTOP_CONTAINER_NETWORK",
		"NETWORK_ARGS=()",
		"--network \"${FSE_DESKTOP_CONTAINER_NETWORK}\"",
		"BUILD_ENV_ARGS=(env DOCKER_BUILDKIT=1)",
		"${BUILD_ENV_ARGS[@]}",
		"/development/fse-desktop-wails-builder-cache/darwin-osxcross-context",
		"detect_sdk_version()",
		"SDK_VERSION=\"$(detect_sdk_version \"$SDK_TARBALL\")\"",
		"SDK_OSXCROSS_VERSION=\"${SDK_VERSION%%.*}\"",
		"SDK_CONTEXT_NAME=\"MacOSX${SDK_OSXCROSS_VERSION}.sdk.tar.xz\"",
		"trap 'rm -f \"$CONTEXT_DIR/MacOSX\"*.sdk.tar*' EXIT",
		"patch-osxcross-sdk.py",
		"fse-desktop-wails-builder:debian12-wails2.10.2-darwin-osxcross",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE",
		"scripts/build-desktop-gui-wails.sh",
	} {
		if !strings.Contains(builderScript, want) {
			t.Fatalf("desktop GUI darwin Wails builder script missing %q:\n%s", want, builderScript)
		}
	}
	for _, forbidden := range []string{
		"curl ",
		"wget ",
		"softwareupdate",
	} {
		if strings.Contains(builderScript, forbidden) {
			t.Fatalf("desktop GUI darwin builder script must not download SDK/tooling, found %q", forbidden)
		}
	}
	for _, want := range []string{
		"scripts/build-desktop-wails-darwin-builder-image.sh <sdk-tarball>",
		"Apple SDK/osxcross-capable isolated builder layer",
		"does not commit or download Apple SDK contents",
		"FSE_DESKTOP_CONTAINER_NETWORK",
		"fse-desktop-wails-builder:debian12-wails2.10.2-darwin-osxcross",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing darwin builder layer note %q:\n%s", want, readme)
		}
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing darwin builder layer note %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUIDarwinBuilderScriptStagesVersionedSDKRootForOsxcross(t *testing.T) {
	root := filepath.Join("..", "..")
	temp := t.TempDir()
	sdkTar := filepath.Join(temp, "MacOSX.sdk.tar")
	writeTinyMacOSSDKTar(t, sdkTar, "26.5")
	contextDir := filepath.Join(temp, "context")

	cmd := exec.Command("bash", "scripts/build-desktop-wails-darwin-builder-image.sh", sdkTar, "fse-test-darwin-builder:stage-only")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"FSE_DARWIN_BUILDER_PREPARE_ONLY=1",
		"FSE_DARWIN_BUILDER_CONTEXT_DIR="+contextDir,
		"FSE_DESKTOP_CONTAINER_RUNTIME=definitely-not-needed-in-prepare-only",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prepare-only darwin builder staging failed: %v\n%s", err, out)
	}

	staged := filepath.Join(contextDir, "MacOSX26.sdk.tar")
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("expected osxcross-compatible staged SDK archive %s: %v\n%s", staged, err, out)
	}
	paths := tarPaths(t, staged)
	if !paths["MacOSX26.sdk/SDKSettings.json"] {
		t.Fatalf("staged SDK archive did not rename top-level SDK root to the osxcross expected major-version SDK: %#v\n%s", paths, out)
	}
	if !paths["MacOSX26.sdk/System/Library/Frameworks/JavaScriptCore.framework/Versions/Current/JavaScriptCore.tbd"] {
		t.Fatalf("staged SDK archive dropped or failed to rewrite SDK symlink entries: %#v\n%s", paths, out)
	}
	if paths["MacOSX26.5.sdk/SDKSettings.json"] {
		t.Fatalf("staged SDK archive kept a patch-version SDK root that osxcross target detection does not search: %#v", paths)
	}
	if paths["MacOSX.sdk/SDKSettings.json"] {
		t.Fatalf("staged SDK archive kept unversioned top-level MacOSX.sdk root that osxcross cannot mv by version: %#v", paths)
	}
}

func writeTinyMacOSSDKTar(t *testing.T, path string, version string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create SDK tar: %v", err)
	}
	defer f.Close()
	w := tar.NewWriter(f)
	defer w.Close()
	entries := []struct {
		name string
		body string
	}{
		{name: "MacOSX.sdk/", body: ""},
		{name: "MacOSX.sdk/SDKSettings.json", body: `{"Version":"` + version + `"}`},
		{name: "MacOSX.sdk/System/Library/Frameworks/JavaScriptCore.framework/Versions/A/JavaScriptCore.tbd", body: "tbd"},
	}
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o755}
		if strings.HasSuffix(entry.name, "/") {
			header.Typeflag = tar.TypeDir
		} else {
			header.Mode = 0o644
			header.Size = int64(len(entry.body))
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", entry.name, err)
		}
		if entry.body != "" {
			if _, err := io.WriteString(w, entry.body); err != nil {
				t.Fatalf("write tar body %s: %v", entry.name, err)
			}
		}
	}
	link := &tar.Header{
		Name:     "MacOSX.sdk/System/Library/Frameworks/JavaScriptCore.framework/Versions/Current/JavaScriptCore.tbd",
		Typeflag: tar.TypeSymlink,
		Linkname: "MacOSX.sdk/System/Library/Frameworks/JavaScriptCore.framework/Versions/A/JavaScriptCore.tbd",
		Mode:     0o777,
	}
	if err := w.WriteHeader(link); err != nil {
		t.Fatalf("write tar symlink header: %v", err)
	}
}

func tarPaths(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open tar %s: %v", path, err)
	}
	defer f.Close()
	r := tar.NewReader(f)
	paths := map[string]bool{}
	for {
		header, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar %s: %v", path, err)
		}
		paths[header.Name] = true
	}
	return paths
}

func TestDesktopGUIDarwinReadinessCheckReportsSDKBuilderAndOutputGates(t *testing.T) {
	root := filepath.Join("..", "..")
	checkScript := readRequiredFile(t, filepath.Join(root, "scripts", "check-desktop-darwin-readiness.sh"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"Usage: scripts/check-desktop-darwin-readiness.sh <version>",
		"FSE_MACOS_SDK_TARBALL",
		"/development/apple-sdk/MacOSX.sdk.tar.xz",
		"DEFAULT_SDK_TARBALL",
		"FSE_DESKTOP_DARWIN_BUILDER_STATUS",
		"/development/logs/file-sync-desktop-darwin/build-darwin-builder.status",
		"active Darwin builder status",
		"builder log progress",
		"last-progress=",
		"pid=",
		"ps -p",
		"stale/dead",
		"fse-desktop-wails-builder:debian12-wails2.10.2-darwin-osxcross",
		"desktop-gui/wails-output/darwin-amd64/fse-desktop.app/Contents/MacOS/fse-desktop",
		"desktop-gui/wails-output/darwin-arm64/fse-desktop.app/Contents/MacOS/fse-desktop",
		"scripts/package-desktop-gui-release.sh",
		"missing SDK tarball",
		"missing Darwin Wails builder image",
		"missing Darwin Wails output",
	} {
		if !strings.Contains(checkScript, want) {
			t.Fatalf("desktop GUI Darwin readiness script missing %q:\n%s", want, checkScript)
		}
	}
	for _, want := range []string{
		"scripts/check-desktop-darwin-readiness.sh <version>",
		"Darwin readiness check",
		"reports the legal Apple SDK tarball, osxcross builder image, Darwin Wails outputs, and all-six release packaging gate without installing host tooling",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing Darwin readiness note %q:\n%s", want, readme)
		}
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing Darwin readiness note %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUIWailsIsolatedBuildScriptUsesContainerAndSixTargets(t *testing.T) {
	root := filepath.Join("..", "..")
	builder := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-gui-wails.sh"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"darwin/amd64) COMPILER_ARGS=(-e CC=o64-clang -e CXX=o64-clang++) ;;",
		"darwin/arm64) COMPILER_ARGS=(-e CC=oa64-clang -e CXX=oa64-clang++) ;;",
		"build_darwin_app_bundle_fallback()",
		"GOOS=darwin",
		"CGO_ENABLED=0 go build",
		"Contents/MacOS",
		"Info.plist",
		"Wails cross-compiling to Mac did not produce build/bin; using osxcross Go app-bundle fallback",
		"Wails cross-compiling to Mac failed; using osxcross Go app-bundle fallback",
		"if ! wails build -platform \"$FSE_DESKTOP_WAILS_PLATFORM\"",
		"go mod tidy",
		"darwin-fallback-main.go",
		"http.FileServer(http.Dir(distDir))",
		"exec.Command(\"open\", url)",
	} {
		if !strings.Contains(builder, want) {
			t.Fatalf("desktop GUI isolated Wails build script missing Darwin osxcross fallback wiring %q:\n%s", want, builder)
		}
	}

	for _, want := range []string{
		"Usage: scripts/build-desktop-gui-wails.sh <version>",
		"if [[ \"$OUTPUT_ROOT\" != /* ]]; then",
		"OUTPUT_ROOT=\"$ROOT/$OUTPUT_ROOT\"",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_AMD64",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS_ARM64",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_ARM64",
		"image_for_platform()",
		"$RUNTIME image inspect",
		"docker run --rm",
		"FSE_DESKTOP_DISABLE_CONTAINER_AUTO_RM",
		"container_create_start_wait()",
		"$RUNTIME create",
		"\"$RUNTIME\" start -a",
		"$RUNTIME wait",
		"FSE_DESKTOP_SKIP_TARGET_PREFLIGHT",
		"FSE_DESKTOP_LINUX_WEBKIT_API",
		"FSE_DESKTOP_WAILS_TAGS_LINUX",
		"webkit2_41",
		"webkit2_40",
		"RUN_RM_ARGS",
		"--read-only",
		"/src:ro",
		"linux/amd64",
		"linux/arm64",
		"windows/amd64",
		"windows/arm64",
		"darwin/amd64",
		"darwin/arm64",
		"desktop-gui/wails-output/${target}",
		"wails build -platform",
		"-tags \"$FSE_DESKTOP_WAILS_TAGS\"",
	} {
		if !strings.Contains(builder, want) {
			t.Fatalf("desktop GUI isolated Wails build script missing %q:\n%s", want, builder)
		}
	}
	for _, forbidden := range []string{
		"apt-get install",
		"apk add",
		"brew install",
		"npm install -g",
		"go install github.com/wailsapp/wails/v2/cmd/wails",
	} {
		if strings.Contains(builder, forbidden) {
			t.Fatalf("desktop GUI Wails build script must not install host/build tooling by default, found %q", forbidden)
		}
	}
	for _, want := range []string{
		"scripts/build-desktop-gui-wails.sh <version>",
		"runs Wails inside caller-supplied Docker/Podman builder images",
		"mounts the repository read-only",
		"writes real Wails outputs under `desktop-gui/wails-output/<target>/`",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN",
		"docker/podman image inspect",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing isolated Wails build note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"Isolated Wails build handoff",
		"default caller-supplied Wails/Node builder image",
		"read-only source mount",
		"desktop-gui/wails-output/<target>/",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_ARM64",
		"docker/podman image inspect",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing isolated Wails build note %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUINativeShellImplementsNonServiceDaemonLaunchAdoptionAndStop(t *testing.T) {
	root := filepath.Join("..", "..")
	appGo := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app.go"))
	frontendContract := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "firstLaunch.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"func (a *App) RequestGUIOwnedNonServiceDaemonLaunch",
		"func (a *App) AdoptGUIOwnedNonServiceDaemon",
		"func (a *App) GetGUIOwnedNonServiceDaemonSession",
		"func (a *App) StopGUIOwnedNonServiceDaemonThroughAPI",
		"exec.Command(command, args...)",
		"start", "FSE_DESKTOP_GUI_OWNED_DAEMON=1",
		"manual-tls", "https://", "X-API-Key", "/v1/stop",
		"gui-owned-daemon-session.json",
		"temporary-session-only",
		"persistent-user-daemon",
	} {
		if !strings.Contains(appGo, want) {
			t.Fatalf("desktop GUI native shell non-service daemon implementation missing %q:\n%s", want, appGo)
		}
	}
	for _, want := range []string{
		"requestGUIOwnedNonServiceDaemonLaunch",
		"adoptGUIOwnedNonServiceDaemon",
		"getGUIOwnedNonServiceDaemonSession",
		"stopGUIOwnedNonServiceDaemonThroughAPI",
		"persistent-user-daemon",
		"temporary-session-only",
	} {
		if !strings.Contains(frontendContract, want) {
			t.Fatalf("desktop GUI frontend non-service daemon contract missing %q:\n%s", want, frontendContract)
		}
	}
	for _, want := range []string{
		"GUI-owned non-service daemon launch/adoption",
		"starts the verified bundled daemon as a separate process",
		"persists a reconnectable session record",
		"stops it through the authenticated encrypted `/v1/stop` daemon API",
		"temporary/session-only mode is allowed to stop with the GUI only when the user explicitly chose it",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing non-service daemon implementation note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"Concrete GUI-owned non-service daemon bridge",
		"verified bundled daemon as a separate process",
		"persisted session record",
		"authenticated encrypted `/v1/stop`",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing non-service daemon bridge note %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUIReleasePackagerCreatesReviewablePerTargetArchives(t *testing.T) {
	root := filepath.Join("..", "..")
	packager := readRequiredFile(t, filepath.Join(root, "scripts", "package-desktop-gui-release.sh"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"Usage: scripts/package-desktop-gui-release.sh <version>",
		"desktop-gui/resources/engine/manifest.json",
		"wails-output",
		"fse-desktop-${VERSION}-${target}.zip",
		"SHA256SUMS",
		"docs-snapshot",
		"zip -qr",
	} {
		if !strings.Contains(packager, want) {
			t.Fatalf("desktop GUI release packager missing %q:\n%s", want, packager)
		}
	}
	for _, forbidden := range []string{
		"npm install",
		"go install",
		"wails build",
		"go build",
	} {
		if strings.Contains(packager, forbidden) {
			t.Fatalf("desktop GUI release packager must package existing GUI/daemon outputs without building or installing tooling, found %q", forbidden)
		}
	}
	for _, want := range []string{
		"scripts/package-desktop-gui-release.sh <version>",
		"packages already-built Wails desktop outputs and verified engine resources",
		"one zip per desktop target under `build/<version>/desktop-gui/`",
		"does not run Wails, npm, Go builds, or install toolchains",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing release packaging contract %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"Desktop GUI release packaging now has a reviewable script",
		"already-built Wails desktop outputs",
		"one zip per target under `build/<version>/desktop-gui/`",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing release packaging progress %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUILinuxInstallerPackagerCreatesDebRpmAndAppImageHandoffs(t *testing.T) {
	root := filepath.Join("..", "..")
	packager := readRequiredFile(t, filepath.Join(root, "scripts", "package-desktop-linux-installers.sh"))
	variantScript := readRequiredFile(t, filepath.Join(root, "scripts", "build-package-desktop-linux-webkit-variants.sh"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"Usage: scripts/package-desktop-linux-installers.sh <version>",
		"linux-amd64",
		"linux-arm64",
		".deb",
		".rpm",
		".AppImage",
		"dpkg-deb",
		"rpmbuild",
		"appimagetool",
		"FSE_DESKTOP_APPIMAGETOOL",
		"FSE_DESKTOP_APPIMAGE_RUNTIME_ARM64",
		"--runtime-file",
		"FSE_DESKTOP_LINUX_INSTALLER_TARGETS",
		"Preserve artifacts for targets that are not part of FSE_DESKTOP_LINUX_INSTALLER_TARGETS.",
		"rm -rf \"$OUT_DIR/.work\"",
		"rm -f \"$OUT_DIR/fse-desktop-${VERSION}-${target}.deb\"",
		"RPM_VERSION=\"${VERSION//-/_}\"",
		"Version: $RPM_VERSION",
		"--target \"$rpm_arch\"",
		"--define \"_topdir $topdir_abs\"",
		"__os_install_post %{nil}",
		"_binary_payload w.ufdio",
		"dpkg-deb -Znone --build",
		"%install",
		"cp -a \"$buildroot_abs/.\" %{buildroot}/",
		"fse-desktop-${VERSION}-${target}.deb",
		"fse-desktop-${VERSION}-${target}.rpm",
		"fse-desktop-${VERSION}-${target}.AppImage",
		"SHA256SUMS",
		"Exec=/opt/fse-desktop/fse-desktop",
		"X-FSE-DaemonMode=independent",
		"--fse-engine-daemon",
		"FSE_DESKTOP_APPIMAGE_ENGINE_MODE=1",
		"fse-desktop.svg",
		"<svg xmlns=",
		"exec \"\\$HERE/opt/fse-desktop/engine/linux/\\$FSE_DESKTOP_APPIMAGE_ARCH/fse\"",
	} {
		if !strings.Contains(packager, want) {
			t.Fatalf("desktop GUI Linux installer packager missing %q:\n%s", want, packager)
		}
	}
	for _, forbidden := range []string{
		"apt-get install",
		"apk add",
		"brew install",
		"npm install",
		"wails build",
		"go build",
	} {
		if strings.Contains(packager, forbidden) {
			t.Fatalf("desktop GUI Linux installer packager must not build/install tooling, found %q", forbidden)
		}
	}
	for _, want := range []string{
		"Usage: scripts/build-package-desktop-linux-webkit-variants.sh <version>",
		"FSE_DESKTOP_LINUX_WEBKIT_VARIANTS:-4.1 4.0",
		"desktop-gui/wails-output-webkit41",
		"desktop-gui/wails-output-webkit40",
		"linux-installers-webkit41",
		"linux-installers-webkit40",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT41",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT40",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64_WEBKIT40",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64_WEBKIT41",
		"FSE_DESKTOP_LINUX_WEBKIT_API=\"$api\"",
		"FSE_DESKTOP_WAILS_BUILDER_IMAGE=\"$image\"",
		"scripts/build-desktop-gui-wails.sh",
		"scripts/package-desktop-linux-installers.sh",
	} {
		if !strings.Contains(variantScript, want) {
			t.Fatalf("desktop GUI Linux WebKit variant handoff missing %q:\n%s", want, variantScript)
		}
	}
	for _, want := range []string{
		"scripts/package-desktop-linux-installers.sh <version>",
		"produces `.deb`, `.rpm`, and `.AppImage` artifacts for `linux-amd64` and `linux-arm64`",
		"requires prebuilt Wails outputs and bundled engine resources",
		"does not run Wails/npm/Go builds or install packaging tools",
		"the daemon independent from the GUI",
		"libgtk-3-0 | libgtk-3-0t64",
		"FSE_DESKTOP_LINUX_WEBKIT_API",
		"libwebkit2gtk-4.1-0",
		"libwebkit2gtk-4.0-37",
		"AppImage engine-only mode",
		"--fse-engine-daemon",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing Linux installer packaging contract %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"Linux installer packaging handoff",
		".deb`, `.rpm`, and `.AppImage`",
		"linux-amd64 and linux-arm64",
		"daemon independently service-owned",
		"libgtk-3-0 | libgtk-3-0t64",
		"FSE_DESKTOP_LINUX_WEBKIT_API",
		"libwebkit2gtk-4.1-0",
		"libwebkit2gtk-4.0-37",
		"AppImage engine-only daemon mode",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing Linux installer packaging note %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUIBundleVerificationGatesLifecycleActions(t *testing.T) {
	root := filepath.Join("..", "..")
	bundleContract := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "bundledEngine.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))

	for _, want := range []string{
		"export type BundledEngineResourceManifest",
		"verifyBundledEngineResourceManifest",
		"verifyBundledEngineResourceEntry",
		"BundledEngineRuntimeGate",
		"bundleVerified: true",
		"bundled engine verification failed",
		"controlVerifiedBundledDaemonLifecycle",
		"controlBundledDaemonLifecycle(settings, action)",
	} {
		if !strings.Contains(bundleContract, want) {
			t.Fatalf("desktop GUI bundled-engine runtime verification gate missing %q:\n%s", want, bundleContract)
		}
	}
	for _, want := range []string{
		"Runtime lifecycle controls must call `verifyBundledEngineResourceManifest`",
		"refuse start/stop/restart/status controls when any packaged daemon binary is missing or has a mismatched checksum",
		"The native shell supplies file-existence and SHA-256 observations; the frontend contract only decides whether lifecycle actions are enabled.",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing bundled-engine verification gate %q:\n%s", want, readme)
		}
	}
}

func TestDesktopGUIFirstLaunchRegistrationPromptsForStartup(t *testing.T) {
	root := filepath.Join("..", "..")
	firstLaunch := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "firstLaunch.ts"))
	nativeShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "nativeShell.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))

	for _, want := range []string{
		"export type FirstLaunchDaemonRegistrationStatus",
		"export type FirstLaunchDaemonRegistrationChoice",
		"registrationRequired",
		"configureStartup",
		"installBundledDaemonForCurrentOS",
		"promptForStartupAtLogin",
		"controlVerifiedBundledDaemonLifecycle",
		"bundleVerified",
		"/v1/service-command",
	} {
		if !strings.Contains(firstLaunch, want) {
			t.Fatalf("desktop GUI first-launch registration contract missing %q:\n%s", want, firstLaunch)
		}
	}
	for _, want := range []string{
		"getFirstLaunchDaemonRegistrationStatus",
		"installBundledDaemonForCurrentOS",
		"promptForStartupAtLogin",
	} {
		if !strings.Contains(nativeShell, want) {
			t.Fatalf("desktop GUI native shell missing first-launch registration bridge %q:\n%s", want, nativeShell)
		}
	}
	for _, want := range []string{
		"First launch daemon setup",
		"firstLaunchStatus",
		"installBundledDaemonForCurrentOS",
		"promptForStartupAtLogin",
		"configureStartup",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI app shell missing first-launch setup UI %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"first-launch daemon registration flow",
		"detects whether the bundled daemon is already registered",
		"asks whether to configure automatic startup/login/start-at-boot",
		"uses the encrypted service-command API after bundle verification",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing first-launch registration note %q:\n%s", want, readme)
		}
	}
	for _, content := range []string{firstLaunch, nativeShell, appShell} {
		for _, forbidden := range []string{
			"child_process",
			"exec(",
			"spawn(",
			"internal/daemon",
			"../cmd/fse",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI first-launch registration must not bypass service/API control via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUINonServiceDaemonLaunchAdoptionContract(t *testing.T) {
	root := filepath.Join("..", "..")
	nativeShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "nativeShell.ts"))
	firstLaunch := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "firstLaunch.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type GUIManagedNonServiceDaemonSession",
		"sessionMode: 'persistent-user-daemon' | 'temporary-session-only'",
		"pid",
		"encryptedApiBaseURL",
		"credentialRef",
		"requestGUIOwnedNonServiceDaemonLaunch",
		"adoptGUIOwnedNonServiceDaemon",
		"stopGUIOwnedNonServiceDaemonThroughAPI",
		"bundleVerified",
	} {
		if !strings.Contains(firstLaunch, want) {
			t.Fatalf("desktop GUI non-service launch contract missing %q:\n%s", want, firstLaunch)
		}
	}
	for _, want := range []string{
		"requestGUIOwnedNonServiceDaemonLaunch",
		"adoptGUIOwnedNonServiceDaemon",
		"getGUIOwnedNonServiceDaemonSession",
		"stopGUIOwnedNonServiceDaemonThroughAPI",
	} {
		if !strings.Contains(nativeShell, want) {
			t.Fatalf("desktop GUI native shell missing non-service daemon bridge %q:\n%s", want, nativeShell)
		}
	}
	for _, want := range []string{
		"GUI-owned non-service daemon",
		"launchGUIOwnedNonServiceDaemon",
		"adoptGUIOwnedNonServiceDaemonSession",
		"Stop through daemon API",
		"persistent-user-daemon",
		"temporary-session-only",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI app shell missing non-service daemon controls %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"GUI-owned non-service daemon launch/adoption",
		"separate non-service process",
		"records PID/API/credential state",
		"closing the GUI does not silently kill sync",
		"temporary/session-only mode",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing non-service daemon launch/adoption note %q", want)
		}
	}
	for _, content := range []string{firstLaunch, nativeShell, appShell} {
		for _, forbidden := range []string{
			"child_process",
			"exec(",
			"spawn(",
			"internal/daemon",
			"../cmd/fse",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI non-service daemon controls must stay native-shell/API bounded, found %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIStartupAndTrayContractUsesDaemonOwnedStatus(t *testing.T) {
	root := filepath.Join("..", "..")
	nativeShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "nativeShell.ts"))
	windowsBridge := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "windowsStartupTray.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type DaemonTrayStatus",
		"export type DaemonStartupIntegrationStatus",
		"getDaemonTrayStatus",
		"getDaemonStartupIntegrationStatus",
		"openGuiFromDaemonTray",
		"daemonOwnedTray",
		"startupEnabled",
	} {
		if !strings.Contains(nativeShell, want) {
			t.Fatalf("desktop GUI native shell missing startup/tray bridge contract %q:\n%s", want, nativeShell)
		}
	}
	for _, want := range []string{
		"export type WindowsStartupTrayState",
		"buildWindowsStartupTrayBridge",
		"platform: 'windows'",
		"getDaemonStartupIntegrationStatus",
		"getDaemonTrayStatus",
		"openGuiFromDaemonTray",
		"controlVerifiedBundledDaemonLifecycle",
		"registerProtocolHandlerCommand",
		"fse-desktop://open",
		"Start-Process",
		"Service Control Manager",
		"no direct daemon process launch",
	} {
		if !strings.Contains(windowsBridge, want) {
			t.Fatalf("desktop GUI Windows startup/tray bridge missing %q:\n%s", want, windowsBridge)
		}
	}
	for _, forbidden := range []string{
		"child_process",
		"exec(",
		"spawn(",
		"internal/daemon",
		"../cmd/fse",
	} {
		if strings.Contains(windowsBridge, forbidden) {
			t.Fatalf("desktop GUI Windows startup/tray bridge must not bypass native shell/service ownership via %q", forbidden)
		}
	}

	macOSBridge := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "macosStartupTray.ts"))
	for _, want := range []string{
		"export type MacOSStartupTrayState",
		"buildMacOSStartupTrayBridge",
		"platform: 'launchd'",
		"getDaemonStartupIntegrationStatus",
		"getDaemonTrayStatus",
		"openGuiFromDaemonTray",
		"controlVerifiedBundledDaemonLifecycle",
		"registerURLSchemeCommand",
		"fse-desktop://open",
		"open 'fse-desktop://open'",
		"LaunchAgent/LaunchDaemon",
		"menu bar status item",
		"no direct daemon process launch",
	} {
		if !strings.Contains(macOSBridge, want) {
			t.Fatalf("desktop GUI macOS startup/tray bridge missing %q:\n%s", want, macOSBridge)
		}
	}
	for _, forbidden := range []string{
		"child_process",
		"exec(",
		"spawn(",
		"internal/daemon",
		"../cmd/fse",
	} {
		if strings.Contains(macOSBridge, forbidden) {
			t.Fatalf("desktop GUI macOS startup/tray bridge must not bypass native shell/service ownership via %q", forbidden)
		}
	}

	linuxBridge := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "linuxStartupTray.ts"))
	for _, want := range []string{
		"export type LinuxStartupTrayState",
		"buildLinuxStartupTrayBridge",
		"platform: 'systemd'",
		"getDaemonStartupIntegrationStatus",
		"getDaemonTrayStatus",
		"openGuiFromDaemonTray",
		"controlVerifiedBundledDaemonLifecycle",
		"renderLinuxDesktopEntryCommand",
		"fse-desktop://open",
		"xdg-open 'fse-desktop://open'",
		"systemd user/system service",
		"StatusNotifier/AppIndicator",
		"no direct daemon process launch",
	} {
		if !strings.Contains(linuxBridge, want) {
			t.Fatalf("desktop GUI Linux startup/tray bridge missing %q:\n%s", want, linuxBridge)
		}
	}
	for _, forbidden := range []string{
		"child_process",
		"exec(",
		"spawn(",
		"internal/daemon",
		"../cmd/fse",
	} {
		if strings.Contains(linuxBridge, forbidden) {
			t.Fatalf("desktop GUI Linux startup/tray bridge must not bypass native shell/service ownership via %q", forbidden)
		}
	}

	for _, want := range []string{
		"Daemon tray and startup",
		"refreshTrayAndStartupStatus",
		"trayStatus",
		"startupIntegrationStatus",
		"openGuiFromDaemonTray",
		"daemonOwnedTray",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI app shell missing startup/tray status UI %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"OS startup/login and tray integration contract",
		"daemon/service owns the tray icon/status",
		"double-clicking the daemon tray icon opens or focuses the separate GUI app",
		"the GUI remains independent and unloadable after use",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing startup/tray contract %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"daemon-owned tray/status integration contract scaffold",
		"open/focus the separate GUI application",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing startup/tray progress note %q:\n%s", want, doc)
		}
	}
	for _, content := range []string{nativeShell, appShell} {
		for _, forbidden := range []string{
			"child_process",
			"exec(",
			"spawn(",
			"internal/daemon",
			"../cmd/fse",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI startup/tray integration must not bypass native shell/service ownership via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIDaemonTrayDoubleClickOpensSeparateGUI(t *testing.T) {
	root := filepath.Join("..", "..")
	trayOpen := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "trayOpen.ts"))
	nativeShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "nativeShell.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type DaemonTrayOpenRequest",
		"handleDaemonTrayOpenRequest",
		"isDaemonTrayOpenRequest",
		"source: 'daemon-tray-double-click' |",
		"fse-desktop://open",
		"--open-from-tray",
		"showMainWindowFromDaemonTray",
		"separate GUI process",
		"no direct daemon process launch",
	} {
		if !strings.Contains(trayOpen, want) {
			t.Fatalf("desktop GUI tray-open bridge missing %q:\n%s", want, trayOpen)
		}
	}
	for _, want := range []string{
		"showMainWindowFromDaemonTray",
		"openGuiFromDaemonTray",
	} {
		if !strings.Contains(nativeShell, want) {
			t.Fatalf("desktop GUI native shell missing tray-open bridge method %q:\n%s", want, nativeShell)
		}
	}
	for _, want := range []string{
		"handleDaemonTrayOpenRequest",
		"isDaemonTrayOpenRequest",
		"--open-from-tray",
		"showMainWindowFromDaemonTray",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI app shell missing tray-open handling %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"daemon tray double-click handoff",
		"receives `fse-desktop://open` or `--open-from-tray`",
		"focuses or shows the already separate GUI process",
		"does not launch the daemon",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing tray double-click handoff note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"daemon tray double-click handoff",
		"focus/show the separate GUI window",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing tray double-click handoff note %q:\n%s", want, doc)
		}
	}
	for _, content := range []string{trayOpen, nativeShell, appShell} {
		for _, forbidden := range []string{
			"child_process",
			"exec(",
			"spawn(",
			"internal/daemon",
			"../cmd/fse",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI tray-open handoff must not bypass native shell/service ownership via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIWailsShellWiresBundleVerificationAndLifecycle(t *testing.T) {
	root := filepath.Join("..", "..")
	wailsConfig := readRequiredFile(t, filepath.Join(root, "desktop-gui", "wails.json"))
	nativeShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "nativeShell.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))

	for _, want := range []string{
		`"name": "File Synchronization Engine"`,
		`"frontend:build": "npm run build"`,
		`"frontend:dev:watcher": "npm run dev"`,
		`"frontend:dir": "."`,
	} {
		if !strings.Contains(wailsConfig, want) {
			t.Fatalf("desktop GUI Wails config missing %q:\n%s", want, wailsConfig)
		}
	}
	for _, want := range []string{
		"export type NativeDesktopShell",
		"readBundledEngineResourceManifest",
		"observeBundledEngineResources",
		"getLocalLifecycleSettings",
		"verifyBundledEngineResourceManifest",
		"controlVerifiedBundledDaemonLifecycle",
	} {
		if !strings.Contains(nativeShell, want) {
			t.Fatalf("desktop GUI native shell bridge missing %q:\n%s", want, nativeShell)
		}
	}
	for _, want := range []string{
		"loadBundledDaemonGate",
		"runBundledDaemonLifecycle",
		"verifyBundledEngineResourceManifest",
		"controlVerifiedBundledDaemonLifecycle",
		"Bundled daemon lifecycle",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI app shell missing native bundle lifecycle wiring %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"`wails.json` records the native shell entrypoint",
		"native shell bridge reads the packaged resource manifest",
		"verifies bundled engine resources before enabling lifecycle controls",
		"calls the encrypted daemon API lifecycle flow",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing Wails/native lifecycle wiring note %q:\n%s", want, readme)
		}
	}
	for _, content := range []string{wailsConfig, nativeShell, appShell} {
		for _, forbidden := range []string{
			"child_process",
			"exec(",
			"spawn(",
			"internal/daemon",
			"../cmd/fse",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI native shell lifecycle wiring must not bypass service/API control via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIAPICertificateTrustTOFUControls(t *testing.T) {
	root := filepath.Join("..", "..")
	apiClient := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type APITrustStatus",
		"export type APITrustCommandRequest",
		"fetchAPITrustStatus",
		"pinActiveAPICertificate",
		"/v1/api/trust",
		"/v1/api/trust-command",
		"pin-active-certificate",
		"certificateSha256",
		"trustedCertificateMatches",
	} {
		if !strings.Contains(apiClient, want) {
			t.Fatalf("desktop GUI daemon API client missing API certificate TOFU control contract %q:\n%s", want, apiClient)
		}
	}
	for _, want := range []string{
		"apiTrustStatus",
		"refreshAPITrustStatus",
		"pinActiveAPICertificateForSelectedHost",
		"API certificate trust",
		"Fetch API trust status",
		"Pin active certificate",
		"trustedCertificateMatches",
		"certificateSha256",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI app shell missing API certificate TOFU control %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"API certificate trust controls",
		"fetch the active certificate fingerprint",
		"pin the active certificate through `/v1/api/trust-command`",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing API trust controls note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"API certificate trust controls",
		"TOFU pairing",
		"without exposing API keys or private key material",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing API trust controls note %q:\n%s", want, doc)
		}
	}
}

func TestDesktopGUILocalEngineControlUsesCommandAPI(t *testing.T) {
	root := filepath.Join("..", "..")
	apiClient := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type PeerCommandRequest",
		"export type FolderCommandRequest",
		"export type DiscoveryCommandRequest",
		"export type TransferCommandRequest",
		"export type MaintenanceScrubRequest",
		"export type WebGUICommandRequest",
		"readDaemonConfig",
		"patchDaemonConfig",
		"sendPeerCommand",
		"sendFolderCommand",
		"sendDiscoveryCommand",
		"sendTransferCommand",
		"runMaintenanceScrub",
		"sendWebGUICommand",
		"/v1/config",
		"/v1/peer-command",
		"/v1/folder-command",
		"/v1/discovery-command",
		"/v1/transfer-command",
		"/v1/maintenance/scrub",
		"/v1/web-gui-command",
		"X-API-Key",
	} {
		if !strings.Contains(apiClient, want) {
			t.Fatalf("desktop GUI local engine control API client missing %q:\n%s", want, apiClient)
		}
	}
	for _, want := range []string{
		"Selected-host engine controls",
		"refreshDaemonConfig",
		"runControlCommand",
		"sendPeerCommand",
		"sendFolderCommand",
		"sendDiscoveryCommand",
		"sendTransferCommand",
		"runMaintenanceScrub",
		"sendWebGUICommand",
		"patchDaemonConfig",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI app shell missing local engine control UI %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"Local engine controls panel",
		"read redacted config",
		"send peer, folder, discovery, transfer, maintenance scrub, web GUI, and non-secret config commands through the daemon API",
		"no manual config-file editing or command-line switches",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing local engine controls note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"full local engine control panel",
		"peer/folder/discovery/transfer/maintenance/web GUI/config command endpoints",
		"without requiring manual config-file edits or command-line switches",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing local engine control progress note %q:\n%s", want, doc)
		}
	}
	for _, content := range []string{apiClient, appShell} {
		for _, forbidden := range []string{
			"child_process",
			"exec(",
			"spawn(",
			"internal/daemon",
			"../cmd/fse",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI local engine controls must stay API-only, found forbidden %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIUsesHostScopedInformationArchitecture(t *testing.T) {
	root := filepath.Join("..", "..")
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"type DesktopUIView",
		"activeView",
		"viewCatalog",
		"host-scoped-shell",
		"host-sidebar",
		"Selected host scope",
		"Overview",
		"Folders",
		"Peers & identity",
		"Transfers",
		"Warnings & logs",
		"Maintenance & backups",
		"Daemon settings",
		"Desktop app settings",
		"Help & details",
		"aria-current={activeView === view.id ? 'page' : undefined}",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI app shell missing host-scoped information architecture contract %q:\n%s", want, appShell)
		}
	}
	for _, forbidden := range []string{
		"<main>\n  <section class=\"connection-card\">",
		"one-page control pile",
	} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI app shell still looks like a one-page control pile via %q", forbidden)
		}
	}
	for _, want := range []string{
		"host-scoped information architecture",
		"left-side host/sidebar shell",
		"overview, folders, peers, transfers, warnings/logs, maintenance/backups, daemon settings, desktop app settings, and help/details",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing host-scoped information architecture note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"host-scoped layout",
		"dedicated areas",
		"Latest information architecture checkpoint",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing information architecture progress note %q:\n%s", want, doc)
		}
	}
	for _, forbidden := range []string{
		"child_process",
		"exec(",
		"spawn(",
		"internal/daemon",
		"../cmd/fse",
	} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI information architecture must stay API-only, found forbidden %q", forbidden)
		}
	}
}

func TestDesktopGUILocalEngineControlShowsPerAreaOperationStatus(t *testing.T) {
	root := filepath.Join("..", "..")
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"type LocalControlArea",
		"type LocalControlOperationState",
		"localControlOperationStates",
		"setLocalControlOperationState",
		"markLocalControlOperationPending",
		"markLocalControlOperationFailed",
		"markLocalControlOperationCompleted",
		"getLocalControlOperationState",
		"renderLocalControlOperationSummary",
		"Peer operation status",
		"Folder operation status",
		"Discovery operation status",
		"Transfer operation status",
		"Maintenance operation status",
		"Web GUI operation status",
		"Config operation status",
		"accepted",
		"pending",
		"failed",
		"completed",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI local engine controls missing per-area operation status contract %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"per-area operation status",
		"accepted, pending, failed, and completed states",
		"peer, folder, discovery, transfer, maintenance, web GUI, and config controls",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing per-area operation status note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"per-area operation status",
		"accepted, pending, failed, and completed local engine operations",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing operation status progress note %q:\n%s", want, doc)
		}
	}
	for _, forbidden := range []string{
		"child_process",
		"exec(",
		"spawn(",
		"internal/daemon",
		"../cmd/fse",
	} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI local engine operation status must stay API-only, found forbidden %q", forbidden)
		}
	}
}

func TestDesktopGUISeparatesDaemonDesktopSettingsAndHelpPages(t *testing.T) {
	root := filepath.Join("..", "..")
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"type DaemonSettingsSection",
		"daemonSettingsSections",
		"type DesktopSettingsSection",
		"desktopSettingsSections",
		"type HelpDetailsSection",
		"helpDetailsSections",
		"Selected-host daemon settings",
		"Daemon identity & API",
		"Metadata, logging, and backup policy",
		"Desktop GUI settings menu",
		"Theme and window behavior",
		"Credential storage and notifications",
		"Dedicated help/details pages",
		"Encryption, pairing, and identity details",
		"Maintenance, repair, and backup details",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI settings/help separation missing %q:\n%s", want, appShell)
		}
	}
	for _, forbidden := range []string{
		"GUI-only preferences such as theme, window behavior, notifications, and credential storage stay separate from selected-host daemon settings.",
		"Advanced option explanations and host-specific help live here so operational views stay focused.",
	} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI settings/help pages still use placeholder-only copy %q", forbidden)
		}
	}
	for _, want := range []string{
		"dedicated selected-host daemon settings page",
		"separate desktop GUI settings menu",
		"dedicated help/details pages",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing settings/help separation note %q:\n%s", want, readme)
		}
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing settings/help separation note %q:\n%s", want, doc)
		}
	}
	for _, forbidden := range []string{
		"child_process",
		"exec(",
		"spawn(",
		"internal/daemon",
		"../cmd/fse",
	} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI settings/help separation must stay API-only, found forbidden %q", forbidden)
		}
	}
}

func TestDesktopGUILocalEngineControlProvidesValidatedForms(t *testing.T) {
	root := filepath.Join("..", "..")
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"validatePeerCommandForm",
		"validateFolderCommandForm",
		"validateDiscoveryCommandForm",
		"validateTransferCommandForm",
		"validateMaintenanceCommandForm",
		"validateWebGUICommandForm",
		"validateConfigPatchForm",
		"peerFormAction",
		"folderFormMode",
		"discoveryFormDisabled",
		"transferFormAction",
		"maintenanceFormFolderID",
		"webGUIFormAction",
		"configPatchLoggingLevel",
		"Peer form",
		"Folder form",
		"Discovery form",
		"Transfer form",
		"Maintenance form",
		"Web GUI form",
		"Config patch form",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI local engine controls missing validated form contract %q:\n%s", want, appShell)
		}
	}
	for _, forbidden := range []string{
		"Peer command</button>",
		"Folder command</button>",
		"Discovery command</button>",
		"Patch logging config</button>",
	} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI local engine controls still expose prototype representative button %q", forbidden)
		}
	}
	for _, want := range []string{
		"validated local engine forms",
		"validate peer IDs, folder IDs, paths, discovery options, transfer scopes, maintenance scope, web GUI actions, and non-secret config patches before sending API requests",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing validated local engine form note %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"validated local engine forms",
		"replace representative command buttons with real peer, folder, discovery, transfer, maintenance, web GUI, and config patch forms",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI architecture doc missing validated local engine form note %q:\n%s", want, doc)
		}
	}
	for _, forbidden := range []string{
		"child_process",
		"exec(",
		"spawn(",
		"internal/daemon",
		"../cmd/fse",
	} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI validated local engine forms must stay API-only, found forbidden %q", forbidden)
		}
	}
}

func TestDesktopGUIPresentsIdentityPairingTextFileAndVisualCodeEntrypoints(t *testing.T) {
	root := filepath.Join("..", "..")
	apiClient := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	pairingContract := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "identityPairing.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type IdentityPairingPackage",
		"export async function generateIdentityPairingPackage",
		"export async function importIdentityPairingPackage",
		"/v1/identity-package",
		"/v1/identity-import",
	} {
		if !strings.Contains(apiClient, want) {
			t.Fatalf("desktop GUI API client missing identity pairing API contract %q:\n%s", want, apiClient)
		}
	}
	for _, want := range []string{
		"exportIdentityPackageAsCopyableText",
		"buildIdentityPackageDownload",
		"buildIdentityPairingQRFallbackPayload",
		"parseImportedIdentityPackageText",
		"animatedPairingCodeDescriptor",
		"buildAnimatedPairingFrames",
		"assembleAnimatedPairingFrames",
		"frameIndex",
		"frameCount",
		"sessionId",
		"payloadSha256",
		"frameKind",
		"parityFrameCount",
		"payloadByteLength",
		"recover missing animated pairing data frames",
		"30% total-frame parity",
		"copyable text",
		"downloadable identity file",
		"QR fallback payload",
		"animated visual code",
		"ordered fragments",
		"single frame is not a complete identity secret",
	} {
		if !strings.Contains(pairingContract, want) {
			t.Fatalf("desktop GUI identity pairing contract missing %q:\n%s", want, pairingContract)
		}
	}
	for _, want := range []string{
		"Identity pairing export/import",
		"pairingGroupID",
		"generatePairingPackage",
		"copyable pairing text",
		"Download identity file",
		"Paste or upload identity file",
		"Import identity package",
		"daemon-owned import execution",
		"QR fallback payload",
		"animated visual code frame list",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing pairing presentation UI %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"copyable text",
		"downloadable identity file",
		"pasted or uploaded identity file",
		"daemon-owned import execution",
		"QR code fallback",
		"animated visual pairing frame contract",
		"payloadSha256",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing pairing presentation note %q", want)
		}
	}
	for _, content := range []string{apiClient, pairingContract, appShell} {
		for _, forbidden := range []string{"api.key", "identityPrivateKey", "privateKey"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI identity pairing presentation must not request or display daemon private/API secrets %q", forbidden)
			}
		}
	}
}

func TestOptionalWebGUIPresentsIdentityPairingTextFileAndImportEntrypoints(t *testing.T) {
	root := filepath.Join("..", "..")
	bundle := readRequiredZipFile(t, filepath.Join(root, "web-gui", "dist", "fse-web-container-default.zip"), "index.html")
	doc := readRequiredFile(t, filepath.Join(root, "docs", "CONFIG.md"))

	for _, want := range []string{
		"Identity pairing export/import",
		"/v1/identity-package",
		"/v1/identity-import",
		"copyable pairing text",
		"Download identity file",
		"Paste or upload identity file",
		"Import identity package",
		"daemon-owned import execution",
	} {
		if !strings.Contains(bundle, want) {
			t.Fatalf("optional web GUI bundle missing identity pairing entrypoint %q:\n%s", want, bundle)
		}
	}
	for _, want := range []string{
		"web GUI identity pairing export/import entrypoints",
		"copyable text",
		"downloadable identity file",
		"pasted or uploaded identity file",
		"daemon-owned import execution",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("config/web GUI docs missing identity pairing note %q", want)
		}
	}
	for _, forbidden := range []string{"api.key", "identityPrivateKey", "privateKey"} {
		if strings.Contains(bundle, forbidden) {
			t.Fatalf("optional web GUI identity pairing entrypoints must not request or display daemon private/API secrets %q", forbidden)
		}
	}
}

func TestDesktopGUIAnimatedPairingUsesConservativeFrameDensity(t *testing.T) {
	root := filepath.Join("..", "..")
	pairingContract := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "identityPairing.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type AnimatedPairingDensityProfile",
		"animatedPairingConservativeDensityProfile",
		"maxPayloadBytesPerFrame: 64",
		"minFrameDisplayMilliseconds: 180",
		"minimumVisualModuleSize: 'large-camera-friendly'",
		"reject dense animated pairing frames",
		"weak cameras and shaky hands",
	} {
		if !strings.Contains(pairingContract, want) {
			t.Fatalf("desktop GUI animated pairing density contract missing %q:\n%s", want, pairingContract)
		}
	}
	for _, want := range []string{
		"conservative animated pairing density",
		"weak cameras and shaky hands",
		"64 bytes per frame",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing animated pairing density UI copy %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"conservative animated pairing density",
		"64 bytes per frame",
		"large-camera-friendly",
		"weak cameras and shaky hands",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing animated pairing density note %q", want)
		}
	}
}

func TestDesktopGUIAnimatedPairingFramesDoNotExposeRawIdentityFragments(t *testing.T) {
	root := filepath.Join("..", "..")
	pairingContract := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "identityPairing.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"privacy-preserving animated pairing fragments",
		"single captured frame cannot reveal a contiguous identity payload chunk",
		"buildPrivacyPreservingAnimatedPairingShard",
		"coefficientsForPrivacyPreservingFrame",
		"avoid identity-basis data shards",
	} {
		if !strings.Contains(pairingContract, want) {
			t.Fatalf("animated pairing privacy contract missing %q:\n%s", want, pairingContract)
		}
	}
	if strings.Contains(pairingContract, "row[frameIndex] = 1;") {
		t.Fatalf("animated pairing frames must not use identity-basis data shards that expose raw payload chunks")
	}
	for _, want := range []string{
		"single captured frame cannot reveal much of the identity code",
		"privacy-preserving animated pairing fragments",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing animated pairing privacy copy %q:\n%s", want, appShell)
		}
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing animated pairing privacy note %q", want)
		}
	}
}

func TestDesktopGUIAnimatedPairingScannerKeepsCollectingAcrossLoops(t *testing.T) {
	root := filepath.Join("..", "..")
	pairingContract := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "identityPairing.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type AnimatedPairingScanState",
		"export type AnimatedPairingScanProgress",
		"createAnimatedPairingScanState",
		"addAnimatedPairingFrameToScan",
		"collectedFrameIndexes",
		"missingFrameIndexes",
		"status: 'collecting' | 'ready' | 'complete'",
		"duplicateFrameCount",
		"continue scanning subsequent animation loops",
		"de-duplicate already-seen frames",
	} {
		if !strings.Contains(pairingContract, want) {
			t.Fatalf("animated pairing scanner contract missing %q:\n%s", want, pairingContract)
		}
	}
	for _, want := range []string{
		"animatedScanProgress",
		"Keep phone pointed at screen until pairing is complete.",
		"continue collecting missing/new frames across animation loops",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing animated scanner progress contract %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"keep scanning subsequent animation loops",
		"de-duplicate already-seen frames",
		"Keep phone pointed at screen until pairing is complete.",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing animated scanner progress note %q", want)
		}
	}
}

func TestDesktopGUIAnimatedPairingCameraScannerUIScreen(t *testing.T) {
	root := filepath.Join("..", "..")
	scanner := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "desktopAnimatedPairingScanner.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type DesktopAnimatedPairingCameraPermissionState",
		"export interface DesktopAnimatedPairingCameraCaptureState",
		"export type DesktopAnimatedPairingScannerScreenModel",
		"buildDesktopAnimatedPairingCameraCaptureState",
		"handleDesktopAnimatedPairingCameraFrame",
		"buildDesktopAnimatedPairingScannerScreen",
		"cameraPermission",
		"decodedFrameCount",
		"rejectedFrameCount",
		"progressPercent",
		"Keep camera pointed at the animated code until pairing is complete.",
		"complete-payload verification before daemon-owned import",
	} {
		if !strings.Contains(scanner, want) {
			t.Fatalf("desktop GUI animated pairing camera scanner contract missing %q:\n%s", want, scanner)
		}
	}
	for _, want := range []string{
		"desktopAnimatedPairingScannerScreen",
		"buildDesktopAnimatedPairingScannerScreen",
		"cameraPermissionMessage",
		"animated camera scanner",
		"Keep camera pointed at the animated code until pairing is complete.",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing camera scanner UI contract %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"desktop animated pairing camera scanner",
		"camera permission",
		"complete-payload verification before daemon-owned import",
		"Keep camera pointed at the animated code until pairing is complete.",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing animated camera scanner note %q", want)
		}
	}
}

func TestDesktopGUIRendersIdentityPairingQRFallbackAsImageModel(t *testing.T) {
	root := filepath.Join("..", "..")
	pairingContract := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "identityPairing.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type IdentityPairingQRImageModel",
		"buildIdentityPairingQRImageModel",
		"qr://fse-identity/",
		"moduleGrid",
		"moduleSizePixels",
		"quietZoneModules",
		"altText",
		"QR fallback image",
		"native/browser QR renderer",
		"never in generic logs/status",
	} {
		if !strings.Contains(pairingContract, want) {
			t.Fatalf("desktop GUI QR fallback image model missing %q:\n%s", want, pairingContract)
		}
	}
	for _, want := range []string{
		"qrFallbackImageModel",
		"buildIdentityPairingQRImageModel",
		"QR fallback image",
		"Show only in the pairing view",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing QR fallback image presentation %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"desktop QR fallback image rendering checkpoint",
		"QR fallback image",
		"native/browser QR renderer",
		"shown only in the pairing view",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing QR fallback image note %q", want)
		}
	}
	for _, forbidden := range []string{"generic logs", "daemon status event", "privateKey", "identityPrivateKey"} {
		if strings.Contains(appShell, forbidden) || strings.Contains(readme, forbidden) || strings.Contains(doc, forbidden) {
			t.Fatalf("desktop QR fallback must not route secret pairing payloads through forbidden surface %q", forbidden)
		}
	}
}

func TestDesktopGUIInstanceRegistryKeepsLocalFirstAndSupportsRemoteEndpoints(t *testing.T) {
	root := filepath.Join("..", "..")
	registry := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "instanceRegistry.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type ManagedDaemonInstance",
		"kind: 'local' | 'remote'",
		"credentialRef",
		"connectionState",
		"ensureLocalInstanceFirst",
		"addRemoteDaemonInstance",
		"removeDaemonInstance",
		"groupDaemonInstances",
		"local instance pinned first",
	} {
		if !strings.Contains(registry, want) {
			t.Fatalf("desktop GUI instance registry missing %q:\n%s", want, registry)
		}
	}
	for _, want := range []string{
		"managedDaemonInstances",
		"selectedInstanceID",
		"Local bundled engine",
		"Remote daemon instances",
		"local instance pinned first",
		"selected host scope",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing instance registry UI contract %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"instance registry",
		"local instance pinned first",
		"remote daemon API endpoints",
		"selected host scope",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing multi-instance registry note %q", want)
		}
	}
	for _, content := range []string{registry, appShell} {
		for _, forbidden := range []string{"apiKey: string", "privateKey", "child_process", "exec(", "spawn("} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI instance registry must not store raw secrets or couple to process launching via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIExpandableInstanceNavigationShowsConnectionStateAndQuickSwitching(t *testing.T) {
	root := filepath.Join("..", "..")
	registry := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "instanceRegistry.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"defaultExpandedInstanceGroups",
		"formatConnectionStateLabel",
		"formatTransferByteSummary",
	} {
		if !strings.Contains(registry, want) {
			t.Fatalf("desktop GUI instance registry missing expandable navigation helper %q:\n%s", want, registry)
		}
	}
	for _, want := range []string{
		"expandedInstanceGroups",
		"toggleInstanceGroup",
		"selectManagedInstance",
		"aria-expanded",
		"connection-state",
		"quick-switch",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing expandable instance navigation contract %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"expandable left navigation panel",
		"connection state",
		"quick switching",
		"local instance pinned first",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing expandable instance navigation note %q", want)
		}
	}
	for _, content := range []string{registry, appShell} {
		for _, forbidden := range []string{"apiKey: string", "privateKey", "child_process", "exec(", "spawn("} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI expandable instance navigation must not store secrets or launch processes via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUISelectedHostScopesWholeInterfaceExceptTopMenu(t *testing.T) {
	root := filepath.Join("..", "..")
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"type SelectedHostContext",
		"selectedHostContext",
		"host-scope-banner",
		"selectedManagedInstance.label",
		"selectedManagedInstance.apiBaseURL",
		"selectedManagedInstance.kind",
		"Selected host connection",
		"Selected host overview",
		"Selected-host folders",
		"Selected-host peers & identity",
		"Selected-host transfers",
		"Selected-host warnings & logs",
		"Selected-host maintenance & backups",
		"Selected-host daemon settings",
		"top GUI menu remains desktop-app scoped",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI selected-host whole-interface scope missing %q:\n%s", want, appShell)
		}
	}
	for _, forbidden := range []string{
		"Local daemon connection",
		"Local engine controls",
		"Local connection credentials",
		"Local host sections",
	} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI selected-host scope still has local-only interface copy %q", forbidden)
		}
	}
	for _, want := range []string{
		"selected-host whole-interface scope",
		"every operational view uses the selected managed daemon instance",
		"only the top GUI menu remains desktop-app scoped",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing selected-host whole-interface scope note %q", want)
		}
	}
	for _, forbidden := range []string{"child_process", "exec(", "spawn(", "apiKey: string"} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI selected-host scoping must not store secrets or launch processes via %q", forbidden)
		}
	}
}

func TestDesktopGUIHostListShowsReadableTransferStatusLayout(t *testing.T) {
	root := filepath.Join("..", "..")
	registry := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "instanceRegistry.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type ManagedDaemonTransferStatusMetrics",
		"buildReadableHostStatusMetrics",
		"Online/offline",
		"Receive remaining",
		"Send remaining",
		"Average receive rate",
		"Average send rate",
	} {
		if !strings.Contains(registry, want) {
			t.Fatalf("desktop GUI instance registry missing readable host status metric %q:\n%s", want, registry)
		}
	}
	for _, want := range []string{
		"host-status-metrics",
		"buildReadableHostStatusMetrics",
		"Online/offline state",
		"Remaining to receive",
		"Remaining to send",
		"Average receive rate",
		"Average send rate",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI host list missing readable transfer status layout %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"readable host status layout",
		"online/offline state, remaining receive/send data, and average receive/send rates",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing readable host status layout note %q", want)
		}
	}
	for _, content := range []string{registry, appShell} {
		for _, forbidden := range []string{"apiKey: string", "privateKey", "child_process", "exec(", "spawn("} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI host status layout must not store secrets or launch processes via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIRemoteInstanceOnboardingSupportsDirectPairingAndIdentitySources(t *testing.T) {
	root := filepath.Join("..", "..")
	registry := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "instanceRegistry.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type RemoteInstanceOnboardingSource",
		"api-endpoint-key",
		"pasted-pairing-code",
		"imported-identity-file",
		"scanned-animated-code",
		"loaded-shared-identity",
		"buildRemoteInstanceOnboardingCandidate",
		"credentialRef",
		"Remote onboarding stores raw API key material only in the native credential vault",
	} {
		if !strings.Contains(registry, want) {
			t.Fatalf("desktop GUI remote onboarding registry contract missing %q:\n%s", want, registry)
		}
	}
	for _, want := range []string{
		"remoteOnboardingSource",
		"remoteOnboardingEndpoint",
		"remoteOnboardingAPIKey",
		"remoteOnboardingPairingCode",
		"remoteOnboardingIdentityFileText",
		"remoteOnboardingAnimatedCodeSummary",
		"remoteOnboardingSharedIdentityID",
		"submitRemoteInstanceOnboarding",
		"Remote instance onboarding form",
		"Direct API endpoint/key",
		"Pasted pairing code",
		"Imported identity file",
		"Scanned animated code",
		"Loaded shared identity",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI remote onboarding shell missing %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"remote instance onboarding",
		"API endpoint/key, pasted pairing code, imported identity file, scanned animated code, or loaded shared identity",
		"raw API keys stay in native credential storage",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing remote onboarding note %q", want)
		}
	}
	for _, content := range []string{registry, appShell} {
		for _, forbidden := range []string{"apiKey: string", "privateKey", "child_process", "exec(", "spawn("} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI remote onboarding must not store secrets or launch processes via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIShowsDiscoveredIdentityPeersProgressively(t *testing.T) {
	root := filepath.Join("..", "..")
	registry := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "instanceRegistry.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type DiscoveredIdentityPeer",
		"export type LoadedSharedIdentityDiscoverySnapshot",
		"buildDiscoveredIdentityPeersFromLoadedSharedIdentity",
		"upsertDiscoveredIdentityPeerInstances",
		"unique peer ID/code",
		"progressive status hydration",
		"capabilities",
		"folders",
		"lastSeenAt",
	} {
		if !strings.Contains(registry, want) {
			t.Fatalf("desktop GUI discovered identity peer registry contract missing %q:\n%s", want, registry)
		}
	}
	for _, want := range []string{
		"loadedSharedIdentityDiscoverySnapshot",
		"autoPopulateLoadedSharedIdentityInstances",
		"hydrateDiscoveredIdentityPeers",
		"newly discovered same-identity peers",
		"Automatically populate reachable same-identity instances",
		"progressively fill in name, status, capabilities, folders, rates, and other metadata",
		"Peer ID/code",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI discovered peer shell missing %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"newly discovered same-identity peers",
		"unique peer ID/code",
		"progressively fills in name, status, capabilities, folders, rates, and other metadata",
		"automatically populate reachable same-identity instances",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing discovered peer progressive hydration note %q", want)
		}
	}
	for _, content := range []string{registry, appShell} {
		for _, forbidden := range []string{"apiKey: string", "privateKey", "child_process", "exec(", "spawn("} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI discovered peer hydration must not store secrets or launch processes via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIShowsPeerPairingNegotiationStatusInHostList(t *testing.T) {
	root := filepath.Join("..", "..")
	registry := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "instanceRegistry.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type PeerPairingNegotiationState",
		"negotiating-identity",
		"exchanging-keys",
		"waiting-on-relay",
		"direct-connection-established",
		"revoked-identity",
		"formatPeerPairingNegotiationStateLabel",
		"buildPeerPairingStatusLine",
	} {
		if !strings.Contains(registry, want) {
			t.Fatalf("desktop GUI registry missing peer pairing/negotiation status contract %q:\n%s", want, registry)
		}
	}
	for _, want := range []string{
		"peer-pairing-status",
		"Peer pairing/negotiation status",
		"Negotiating identity",
		"Exchanging keys",
		"Waiting on relay/mesh hop",
		"Direct connection established",
		"Revoked identity",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI host list missing peer pairing status UI %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"pairing/negotiation status",
		"discovered, connecting, negotiating identity, exchanging keys, waiting on relay/mesh hop, direct connection established, paired, failed, revoked identity, and offline states",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing peer pairing status note %q", want)
		}
	}
	for _, content := range []string{registry, appShell} {
		for _, forbidden := range []string{"apiKey: string", "privateKey", "child_process", "exec(", "spawn("} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI peer pairing status must not store secrets or launch processes via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIRemoteMeshSettingsPlanUsesDurablePerNodeDocuments(t *testing.T) {
	root := filepath.Join("..", "..")
	registry := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "instanceRegistry.ts"))
	apiClient := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type RemoteMeshSettingsDocument",
		"export type RemoteMeshPendingSettingsChange",
		"export type OfflineRemoteSettingsEdit",
		"buildRemoteMeshSettingsDocument",
		"queueRemoteMeshSettingsChange",
		"buildOfflineRemoteSettingsEdit",
		"offline edits remain durable local pending changes",
		"node owns exactly one canonical settings document",
		"durable per-target pending command records",
		"config file as an editable local import/export surface",
		"idempotencyKey",
	} {
		if !strings.Contains(registry, want) {
			t.Fatalf("desktop GUI remote mesh settings registry contract missing %q:\n%s", want, registry)
		}
	}
	for _, want := range []string{
		"remote-mesh-settings-plan",
		"Trusted eventually-consistent mesh management",
		"remoteMeshDocuments",
		"remoteMeshPendingChanges",
		"refreshRemoteMeshStatus",
		"Remote mesh document status",
		"Pending remote settings changes",
		"online, relay-reachable, and offline instances",
		"per-node settings document",
		"durable pending changes",
		"offline instances can be inspected or queued for later adoption",
		"config file remains local import/export",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI remote mesh settings shell missing %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"export type MeshSettingsResponse",
		"export type MeshSettingsCommandResponse",
		"fetchRemoteMeshSettings",
		"queueRemoteMeshSettingsCommand",
		"/v1/mesh/settings",
		"/v1/mesh/settings-command",
	} {
		if !strings.Contains(apiClient, want) {
			t.Fatalf("desktop GUI daemon API client missing remote mesh settings API helper %q:\n%s", want, apiClient)
		}
	}
	for _, want := range []string{
		"trusted eventually-consistent mesh",
		"each node owns exactly one canonical settings document",
		"durable per-target pending command records",
		"config file as a local import/export surface",
		"offline instances can be inspected or queued for later adoption",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing remote mesh settings note %q", want)
		}
	}
	for _, content := range []string{registry, appShell} {
		for _, forbidden := range []string{"apiKey: string", "privateKey", "child_process", "exec(", "spawn("} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI remote mesh settings must not store secrets or launch processes via %q", forbidden)
			}
		}
	}
}

func TestDesktopGUIRemoteCredentialVaultUsesNativeSecretStores(t *testing.T) {
	root := filepath.Join("..", "..")
	vault := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "credentialVault.ts"))
	nativeShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "nativeShell.ts"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type CredentialVaultPlatform = 'windows' | 'macos' | 'linux'",
		"Windows Credential Manager",
		"macOS Keychain",
		"Freedesktop Secret Service",
		"credentialRef",
		"storeRemoteInstanceCredential",
		"resolveRemoteInstanceCredential",
		"deleteRemoteInstanceCredential",
		"api key material is never persisted in the instance registry",
	} {
		if !strings.Contains(vault, want) {
			t.Fatalf("desktop GUI credential vault contract missing %q:\n%s", want, vault)
		}
	}
	for _, want := range []string{
		"storeRemoteInstanceCredential",
		"resolveRemoteInstanceCredential",
		"deleteRemoteInstanceCredential",
	} {
		if !strings.Contains(nativeShell, want) {
			t.Fatalf("native shell bridge missing credential vault method %q:\n%s", want, nativeShell)
		}
	}
	for _, want := range []string{
		"secure credential storage",
		"Windows Credential Manager",
		"macOS Keychain",
		"Freedesktop Secret Service",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing credential storage note %q", want)
		}
	}
	for _, forbidden := range []string{"apiKey: string", "localStorage", "sessionStorage"} {
		if strings.Contains(vault, forbidden) {
			t.Fatalf("desktop GUI credential vault must not expose raw persistent API-key storage via %q", forbidden)
		}
	}
}

func TestDesktopGUIRemoteFolderBrowseUsesSelectedHostAPI(t *testing.T) {
	root := filepath.Join("..", "..")
	apiClient := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	doc := readRequiredFile(t, filepath.Join(root, "docs", "DESKTOP_GUI_ARCHITECTURE.md"))

	for _, want := range []string{
		"export type FilesystemBrowseResponse",
		"browseFilesystemDirectories",
		"/v1/filesystem/browse",
	} {
		if !strings.Contains(apiClient, want) {
			t.Fatalf("desktop GUI API client missing remote folder browse helper %q:\n%s", want, apiClient)
		}
	}
	for _, want := range []string{
		"browseRemoteFolderTree",
		"remoteBrowsePath",
		"remoteBrowseEntries",
		"canBrowseSelectedHostFilesystem",
		"selected host is offline or unreachable for live folder-tree queries",
		"disabled={remoteBrowseLoading || !canBrowseSelectedHostFilesystem",
		"Remote folder browse",
		"manual path entry remains available when the selected host is offline",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing selected-host folder browse UI %q:\n%s", want, appShell)
		}
	}
	for _, want := range []string{
		"live remote folder-tree browsing",
		"selected remote host's authenticated API",
		"manual path entry remains available when offline",
	} {
		if !strings.Contains(readme, want) || !strings.Contains(doc, want) {
			t.Fatalf("desktop GUI docs missing remote folder browse note %q", want)
		}
	}
	for _, forbidden := range []string{"child_process", "exec(", "spawn("} {
		if strings.Contains(appShell, forbidden) {
			t.Fatalf("desktop GUI remote folder browse must not shell out via %q", forbidden)
		}
	}
}

func TestDesktopGUIScaffoldPreservesDaemonProcessBoundary(t *testing.T) {
	root := filepath.Join("..", "..")
	packageJSON := readRequiredFile(t, filepath.Join(root, "desktop-gui", "package.json"))
	readme := readRequiredFile(t, filepath.Join(root, "desktop-gui", "README.md"))
	apiClient := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	appShell := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))

	for _, want := range []string{
		"@filesyncengine/desktop-gui",
		"typecheck",
		"wails:dev",
	} {
		if !strings.Contains(packageJSON, want) {
			t.Fatalf("desktop GUI package scaffold missing %q:\n%s", want, packageJSON)
		}
	}
	for _, want := range []string{
		"separate daemon process",
		"closing the GUI unloads only the GUI",
		"authenticated encrypted API",
		"No daemon code is imported into this frontend scaffold",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("desktop GUI README missing process-boundary contract %q:\n%s", want, readme)
		}
	}
	for _, want := range []string{
		"export type DaemonConnectionSettings",
		"apiBaseURL",
		"apiKey",
		"fetch(",
		"X-API-Key",
	} {
		if !strings.Contains(apiClient, want) {
			t.Fatalf("desktop GUI API client missing decoupled API contract %q:\n%s", want, apiClient)
		}
	}
	for _, want := range []string{
		"Selected host connection",
		"apiBaseURL",
		"connectToDaemon",
	} {
		if !strings.Contains(appShell, want) {
			t.Fatalf("desktop GUI shell missing daemon connection UI contract %q:\n%s", want, appShell)
		}
	}
	for _, content := range []string{packageJSON, readme, apiClient, appShell} {
		for _, forbidden := range []string{
			"../cmd/fse",
			"internal/daemon",
			"child_process",
			"exec(",
			"spawn(",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("desktop GUI scaffold must not couple directly to daemon implementation or process launching via %q", forbidden)
			}
		}
	}
}
