package main

// desktopAppVersion is the running GUI version. Release and CI builds rewrite
// this file immediately before `wails build` via scripts/stamp-desktop-gui-version.sh.
// Do not bump a date.build version in git.
var desktopAppVersion = "0.0.0-prototype"
