package modload

import (
	"fmt"
	"os"
	"path/filepath"
)

var (
	modRoot     string
	initialized bool
)

func ModRoot() string {
	if !HasModRoot() {
		die("cannot find main module; see 'go help modules'")
	}
	return modRoot
}

func HasModRoot() bool {
	if initialized {
		return modRoot != ""
	}
	initialized = true

	cwd, err := os.Getwd()
	if err != nil {
		die(err.Error())
	}

	for {
		modPath := filepath.Join(cwd, "go.mod")
		if _, err := os.Stat(modPath); err == nil {
			modRoot = cwd
			return true
		} else if cwd == "/" {
			return false
		}
		cwd = filepath.Dir(cwd)
	}
}

func die(msg string) {
	_, _ = fmt.Fprintf(os.Stderr, "modfile: %s\n", msg)
	os.Exit(1)
}
