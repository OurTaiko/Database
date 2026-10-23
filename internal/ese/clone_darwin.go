package ese

import (
	"os"

	"golang.org/x/sys/unix"
)

// cloneFile makes dst an APFS copy-on-write clone of src, so importing the
// audio library takes no extra disk space. It falls back to a plain copy.
func cloneFile(src, dst string) error {
	if err := unix.Clonefile(src, dst, 0); err == nil {
		return os.Chmod(dst, 0600)
	}
	return copyFile(src, dst)
}
