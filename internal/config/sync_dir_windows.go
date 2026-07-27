//go:build windows

package config

// Windows does not support syncing directory handles through os.File.Sync. The
// replacement file itself has already been flushed before rename, so directory
// syncing must be a successful no-op rather than breaking TLS/config bootstrap.
func syncDir(string) error {
	return nil
}
