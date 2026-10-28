//go:build windows

package auth

// NTFS journals renames; Windows does not expose directory fsync through os.File.
func syncSessionDirectory(string) error { return nil }
