package catalog

import (
	"io/fs"
	"os"
	"path/filepath"
)

// openFSFile opens a file relative to the given root path.
func openFSFile(root, name string) (fs.File, error) {
	return os.Open(filepath.Join(root, filepath.FromSlash(name)))
}
