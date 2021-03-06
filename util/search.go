package util

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrNotExecutable = errors.New("not executable")
	ErrNotFound      = errors.New("not found")
)

// DontSearch determines whether the path to an external command should be
// taken literally and not searched.
func DontSearch(exe string) bool {
	return exe == ".." || strings.ContainsRune(exe, filepath.Separator)
}

// IsExecutable determines whether path refers to an executable file.
func IsExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	fm := fi.Mode()
	return !fm.IsDir() && (fm&0111 != 0)
}
