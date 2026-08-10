// Package unixsocket maps logical Unix socket names to paths accepted by both
// macOS and Linux kernels.
package unixsocket

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Darwin's sockaddr_un.sun_path is 104 bytes and Linux's is 108, including
// the trailing NUL. Keep room for collision suffixes added by Shelley.
const maxPortablePathBytes = 99

// Path returns requested unchanged when it is portable. Longer logical paths
// map deterministically into a short per-user directory under /tmp, allowing
// both the listener and its clients to derive the same physical socket path.
func Path(requested string) (string, error) {
	if requested == "" {
		return "", errors.New("empty Unix socket path")
	}
	if len([]byte(requested)) <= maxPortablePathBytes {
		return requested, nil
	}
	absolute, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("resolve Unix socket path: %w", err)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absolute)))
	name := hex.EncodeToString(digest[:16]) + ".sock"
	directory := filepath.Join("/tmp", fmt.Sprintf("shelley-sockets-%d", os.Getuid()))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create shortened Unix socket directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("secure shortened Unix socket directory: %w", err)
	}
	short := filepath.Join(directory, name)
	if len([]byte(short)) > maxPortablePathBytes {
		return "", fmt.Errorf("shortened Unix socket path is still too long: %q", short)
	}
	return short, nil
}
