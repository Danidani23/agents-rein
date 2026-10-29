package owner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// ErrRunning is the refusal to listen where an owner already answers.
var ErrRunning = errors.New("a background process already runs for this home")

// maximumSocketPath is the longest socket path used in place: a Unix
// socket's path holds about a hundred bytes, 104 on macOS and 108 on Linux
// with its end (https://man7.org/linux/man-pages/man7/unix.7.html).
const maximumSocketPath = 100

// SocketPath is where the owner of home listens: owner.sock in home, or,
// when that path is too long for a Unix socket, a name made from home in
// the system's temporary folder.
func SocketPath(home string) string {
	path := filepath.Join(home, "owner.sock")
	if len(path) <= maximumSocketPath {
		return path
	}
	sum := sha256.Sum256([]byte(filepath.Clean(home)))
	return filepath.Join(os.TempDir(), "rein-"+hex.EncodeToString(sum[:8])+".sock")
}

// Listen opens the socket, open to this account alone: by its mode on
// macOS and Linux, by the access list of the folder it is in on Windows,
// where a mode is only a read-only flag. A socket an owner that is gone
// left behind is taken over; one that an owner answers is refused with
// ErrRunning.
func Listen(socket string) (net.Listener, error) {
	listener, err := net.Listen("unix", socket)
	if err != nil {
		_, identifyErr := Identify(socket)
		if identifyErr == nil {
			return nil, ErrRunning
		}
		removeErr := os.Remove(socket)
		if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return nil, err
		}
		listener, err = net.Listen("unix", socket)
		if err != nil {
			return nil, err
		}
	}
	if runtime.GOOS == "windows" {
		return listener, nil
	}
	err = os.Chmod(socket, 0o600)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}
