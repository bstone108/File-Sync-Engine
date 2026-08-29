//go:build darwin && cgo

#ifndef FSE_SPARKLE_DARWIN_H
#define FSE_SPARKLE_DARWIN_H

int FSESparkleStart(void);
int FSESparkleCheck(void);
int FSESparkleRestartNow(void);
int FSESparklePostpone(void);
int FSESparkleReady(void);
const char *FSESparkleVersion(void);
const char *FSESparkleMessage(void);
int FSESparkleError(void);

#endif
