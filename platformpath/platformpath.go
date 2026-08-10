// Package platformpath normalizes existing filesystem paths across supported
// platforms. In particular, macOS exposes /var as a symlink to /private/var,
// while tools such as Git report the physical path.
package platformpath

import "path/filepath"

// Existing returns the absolute, symlink-resolved spelling of path.
func Existing(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}
