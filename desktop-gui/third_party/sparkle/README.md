Sparkle.framework is downloaded at macOS build time by
`scripts/fetch-sparkle-framework.sh` and is not committed.

Native macOS Wails builds link against Sparkle.framework here and copy it into
`Contents/Frameworks` of fse-desktop.app. Release jobs sign the per-arch appcast
with Sparkle `sign_update` and `SPARKLE_EDDSA_PRIVATE_KEY`.
