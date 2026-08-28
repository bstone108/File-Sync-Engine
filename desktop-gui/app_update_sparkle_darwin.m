//go:build darwin && cgo

#import "app_update_sparkle_darwin.h"

#import <Foundation/Foundation.h>
#import <Sparkle/Sparkle.h>

#include <string.h>

extern void fseSparkleStatusChanged(void);

static NSString *FSESparkleFeedURL(void) {
#if defined(__arm64__) || defined(__aarch64__)
    return @"https://github.com/bstone108/File-Sync-Engine/releases/latest/download/appcast-darwin-arm64.xml";
#else
    return @"https://github.com/bstone108/File-Sync-Engine/releases/latest/download/appcast-darwin-amd64.xml";
#endif
}

@interface FSESparkleDriver : NSObject <SPUUserDriver, SPUUpdaterDelegate>
@property (nonatomic, strong) SPUUpdater *updater;
@property (nonatomic, copy) void (^installReply)(SPUUserUpdateChoice);
@property (nonatomic, copy) NSString *availableVersion;
@property (nonatomic, copy) NSString *message;
@property (nonatomic, assign) BOOL ready;
@property (nonatomic, assign) BOOL failed;
@end

@implementation FSESparkleDriver

- (void)notifyGo {
    fseSparkleStatusChanged();
}

- (void)showUpdatePermissionRequest:(SPUUpdatePermissionRequest *)request reply:(void (^)(SUUpdatePermissionResponse *))reply {
    (void)request;
    reply([[SUUpdatePermissionResponse alloc] initWithAutomaticUpdateChecks:YES sendSystemProfile:NO]);
}

- (void)showUserInitiatedUpdateCheckWithCancellation:(void (^)(void))cancellation {
    (void)cancellation;
}

- (void)showUpdateFoundWithAppcastItem:(SUAppcastItem *)appcastItem state:(SPUUserUpdateState *)state reply:(void (^)(SPUUserUpdateChoice))reply {
    (void)state;
    if (appcastItem.informationOnlyUpdate) {
        self.failed = YES;
        self.ready = NO;
        self.message = @"macOS update is informational only; Sparkle will not use a download-link fallback.";
        [self notifyGo];
        reply(SPUUserUpdateChoiceDismiss);
        return;
    }
    self.failed = NO;
    self.availableVersion = appcastItem.displayVersionString ?: appcastItem.versionString ?: @"";
    self.message = [NSString stringWithFormat:@"Downloading macOS update %@ with Sparkle.", self.availableVersion];
    [self notifyGo];
    reply(SPUUserUpdateChoiceInstall);
}

- (void)showUpdateReleaseNotesWithDownloadData:(SPUDownloadData *)downloadData {
    (void)downloadData;
}

- (void)showUpdateReleaseNotesFailedToDownloadWithError:(NSError *)error {
    (void)error;
}

- (void)showUpdateNotFoundWithError:(NSError *)error acknowledgement:(void (^)(void))acknowledgement {
    (void)error;
    acknowledgement();
}

- (void)showUpdaterError:(NSError *)error acknowledgement:(void (^)(void))acknowledgement {
    self.failed = YES;
    self.ready = NO;
    self.message = error.localizedDescription ?: @"Sparkle update failed.";
    [self notifyGo];
    acknowledgement();
}

- (void)showDownloadInitiatedWithCancellation:(void (^)(void))cancellation {
    (void)cancellation;
    self.message = @"Downloading the matching notarized macOS update.";
    [self notifyGo];
}

- (void)showDownloadDidReceiveExpectedContentLength:(uint64_t)expectedContentLength {
    (void)expectedContentLength;
}

- (void)showDownloadDidReceiveDataOfLength:(uint64_t)length {
    (void)length;
}

- (void)showDownloadDidStartExtractingUpdate {
    self.message = @"Preparing the staged macOS update.";
    [self notifyGo];
}

- (void)showExtractionReceivedProgress:(double)progress {
    (void)progress;
}

- (void)showReadyToInstallAndRelaunch:(void (^)(SPUUserUpdateChoice))reply {
    self.installReply = [reply copy];
    self.ready = YES;
    self.failed = NO;
    self.message = [NSString stringWithFormat:@"macOS update %@ is staged. Restart now or later.", self.availableVersion ?: @""];
    [self notifyGo];
}

- (void)showInstallingUpdateWithApplicationTerminated:(BOOL)applicationTerminated retryTerminatingApplication:(void (^)(void))retryTerminatingApplication {
    (void)applicationTerminated;
    (void)retryTerminatingApplication;
}

- (void)showUpdateInstalledAndRelaunched:(BOOL)relaunched acknowledgement:(void (^)(void))acknowledgement {
    (void)relaunched;
    acknowledgement();
}

- (void)showUpdateInFocus {
}

- (void)dismissUpdateInstallation {
    self.installReply = nil;
}

- (NSString *)feedURLStringForUpdater:(SPUUpdater *)updater {
    (void)updater;
    return FSESparkleFeedURL();
}

@end

static FSESparkleDriver *FSESparkleSharedDriver(void) {
    static FSESparkleDriver *driver;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        driver = [FSESparkleDriver new];
    });
    return driver;
}

static int FSESparkleRunOnMain(void (^block)(void)) {
    if ([NSThread isMainThread]) {
        block();
        return 0;
    }
    dispatch_sync(dispatch_get_main_queue(), block);
    return 0;
}

int FSESparkleStart(void) {
    __block int ok = 0;
    FSESparkleRunOnMain(^{
        FSESparkleDriver *driver = FSESparkleSharedDriver();
        if (driver.updater != nil) {
            ok = 1;
            return;
        }
        NSBundle *bundle = [NSBundle mainBundle];
        SPUUpdater *updater = [[SPUUpdater alloc] initWithHostBundle:bundle applicationBundle:bundle userDriver:driver delegate:driver];
        NSError *error = nil;
        if (![updater startUpdater:&error]) {
            driver.failed = YES;
            driver.message = error.localizedDescription ?: @"Sparkle failed to start.";
            ok = 0;
            return;
        }
        driver.updater = updater;
        ok = 1;
    });
    return ok;
}

int FSESparkleCheck(void) {
    FSESparkleStart();
    FSESparkleRunOnMain(^{
        FSESparkleDriver *driver = FSESparkleSharedDriver();
        [driver.updater checkForUpdatesInBackground];
    });
    return 1;
}

int FSESparkleRestartNow(void) {
    __block int ok = 0;
    FSESparkleRunOnMain(^{
        FSESparkleDriver *driver = FSESparkleSharedDriver();
        void (^reply)(SPUUserUpdateChoice) = driver.installReply;
        if (reply == nil) {
            ok = 0;
            return;
        }
        driver.installReply = nil;
        reply(SPUUserUpdateChoiceInstall);
        ok = 1;
    });
    return ok;
}

int FSESparklePostpone(void) {
    FSESparkleRunOnMain(^{
        FSESparkleDriver *driver = FSESparkleSharedDriver();
        void (^reply)(SPUUserUpdateChoice) = driver.installReply;
        driver.installReply = nil;
        driver.ready = NO;
        if (reply != nil) {
            reply(SPUUserUpdateChoiceDismiss);
        }
    });
    return 1;
}

int FSESparkleReady(void) {
    return FSESparkleSharedDriver().ready ? 1 : 0;
}

int FSESparkleError(void) {
    return FSESparkleSharedDriver().failed ? 1 : 0;
}

const char *FSESparkleVersion(void) {
    NSString *version = FSESparkleSharedDriver().availableVersion ?: @"";
    return strdup(version.UTF8String);
}

const char *FSESparkleMessage(void) {
    NSString *message = FSESparkleSharedDriver().message ?: @"";
    return strdup(message.UTF8String);
}
