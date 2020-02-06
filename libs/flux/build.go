package flux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/influxdata/pkg-config/internal/modfile"
	"github.com/influxdata/pkg-config/internal/modload"
	"go.uber.org/zap"
)

type Library struct {
	Version string
	Dir     string
}

const modulePath = "github.com/influxdata/flux"

func Configure(ctx context.Context, logger *zap.Logger) (*Library, error) {
	modroot := modload.ModRoot()
	data, err := ioutil.ReadFile(filepath.Join(modroot, "go.mod"))
	if err != nil {
		return nil, err
	}

	module, err := modfile.Parse(modroot, data, nil)
	if err != nil {
		return nil, err
	}

	// If the module we are building is flux itself, then set
	// the directory to the one where the module root is located
	// so we are building locally.
	if module.Module.Mod.Path == modulePath {
		logger.Info("Flux module is the main module", zap.String("modroot", modroot))
		dir := filepath.Join(modroot, "libflux")
		v, err := getVersion(modroot)
		if err != nil {
			return nil, err
		}
		return &Library{Dir: dir, Version: v}, nil
	}
	return nil, errors.New("implement me")
}

func (l *Library) Install(ctx context.Context, logger *zap.Logger) error {
	logger.Info("Running cargo build", zap.String("dir", l.Dir))
	cmd := exec.Command("cargo", "build", "--release")
	cmd.Dir = l.Dir
	if err := cmd.Run(); err != nil {
		return err
	}

	libdir := filepath.Join(l.Dir, "lib")
	logger.Info("Creating libdir", zap.String("libdir", libdir))
	if err := os.MkdirAll(libdir, 0755); err != nil {
		return err
	}

	libnames := []string{"flux", "libstd"}
	for _, name := range libnames {
		basename := fmt.Sprintf("lib%s.a", name)
		src := filepath.Join(l.Dir, "target", "release", basename)
		dst := filepath.Join(libdir, basename)
		logger.Info("Linking library to libdir", zap.String("src", src), zap.String("dst", dst))
		if _, err := os.Stat(dst); err == nil {
			_ = os.Remove(dst)
		}

		if err := os.Link(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func (l *Library) WritePackageConfig(w io.Writer) error {
	if l.Dir == "" {
		return errors.New("flux library directory not set")
	}

	_, _ = fmt.Fprintf(w, "prefix=%s\n", l.Dir)
	_, _ = io.WriteString(w, `exec_prefix=${prefix}
libdir=${exec_prefix}/lib
includedir=${prefix}/include

Name: Flux
`)
	_, _ = fmt.Fprintf(w, "Version: %s\n", l.Version)
	_, _ = io.WriteString(w, `Description: Library for the InfluxData Flux engine
Libs: -L${libdir} -lflux -llibstd
Cflags: -I${includedir}
`)
	return nil
}

func getVersion(dir string) (string, error) {
	cmd := exec.Command("git", "describe")
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	versionStr := strings.TrimSpace(string(out))

	re := regexp.MustCompile(`(v\d+\.\d+\.\d+)(-.*)?`)
	m := re.FindStringSubmatch(versionStr)
	if m == nil {
		return "", fmt.Errorf("invalid tag version format: %s", versionStr)
	}

	if m[2] == "" {
		return m[1][1:], nil
	}

	v, err := semver.NewVersion(m[1])
	if err != nil {
		return "", err
	}
	*v = v.IncMinor()
	return v.String(), nil
}
