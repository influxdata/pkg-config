package flux

import (
	"context"
	"encoding/json"
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
	"github.com/influxdata/pkg-config/internal/module"
	"go.uber.org/zap"
)

type Library struct {
	Path    string
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

	ver, dir, err := findModule(module, logger)
	if err != nil {
		return nil, err
	}
	return &Library{Path: ver.Path, Version: ver.Version, Dir: dir}, nil
}

func (l *Library) Install(ctx context.Context, logger *zap.Logger) error {
	if err := l.copyIfReadOnly(ctx, logger); err != nil {
		return err
	}

	cmd := exec.Command("cargo", "build", "--release")
	cmd.Dir = filepath.Join(l.Dir, "libflux")
	logger.Info("Running cargo build", zap.String("dir", cmd.Dir))
	if err := cmd.Run(); err != nil {
		return err
	}

	libdir := filepath.Join(cmd.Dir, "lib")
	logger.Info("Creating libdir", zap.String("libdir", libdir))
	if err := os.MkdirAll(libdir, 0755); err != nil {
		return err
	}

	libnames := []string{"flux", "libstd"}
	for _, name := range libnames {
		basename := fmt.Sprintf("lib%s.a", name)
		src := filepath.Join(cmd.Dir, "target", "release", basename)
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

// copyIfReadOnly will copy the module to another location if the directory is read only.
func (l *Library) copyIfReadOnly(ctx context.Context, logger *zap.Logger) error {
	if st, err := os.Stat(l.Dir); err != nil {
		return err
	} else if st.Mode()&0200 != 0 {
		return nil
	}

	// Find the go cache as this is a safe place for us to copy the sources to.
	cache, err := getGoCache()
	if err != nil {
		return err
	}

	// Determine the source path. If the directory already exists,
	// then we have already copied the files.
	srcdir := filepath.Join(cache, "pkgconfig", l.Path+"@"+l.Version)
	if _, err := os.Stat(srcdir); err == nil {
		l.Dir = srcdir
		return nil
	}

	// Copy over the directory.
	if err := filepath.Walk(l.Dir, func(path string, info os.FileInfo, err error) error {
		relpath, err := filepath.Rel(l.Dir, path)
		if err != nil {
			return err
		}

		targetpath := filepath.Join(srcdir, relpath)
		if info.IsDir() {
			return os.MkdirAll(targetpath, 0755)
		}

		r, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()

		w, err := os.Create(targetpath)
		if err != nil {
			return err
		}

		if _, err := io.Copy(w, r); err != nil {
			return err
		}
		return w.Close()
	}); err != nil {
		return err
	}

	l.Dir = srcdir
	return nil
}

func (l *Library) WritePackageConfig(w io.Writer) error {
	prefix := filepath.Join(l.Dir, "libflux")
	_, _ = fmt.Fprintf(w, "prefix=%s\n", prefix)
	_, _ = io.WriteString(w, `exec_prefix=${prefix}
libdir=${exec_prefix}/lib
includedir=${prefix}/include

Name: Flux
`)
	_, _ = fmt.Fprintf(w, "Version: %s\n", l.Version[1:])
	_, _ = io.WriteString(w, `Description: Library for the InfluxData Flux engine
Libs: -L${libdir} -lflux -llibstd
Cflags: -I${includedir}
`)
	return nil
}

// findModule will find the module in the module file and instantiate
// a module.Version that points to a local copy of the module.
func findModule(mod *modfile.File, logger *zap.Logger) (module.Version, string, error) {
	if mod.Module.Mod.Path == modulePath {
		modroot := modload.ModRoot()
		logger.Info("Flux module is the main module", zap.String("modroot", modroot))
		v, err := getVersion(modroot)
		if err != nil {
			return module.Version{}, "", err
		}
		return module.Version{Version: v}, modroot, nil
	}

	// Attempt to find the module in the list of replace values.
	for _, replace := range mod.Replace {
		if replace.Old.Path == modulePath {
			return getModule(replace.New, logger)
		}
	}

	// Attempt to find the module in the normal dependencies.
	for _, m := range mod.Require {
		if m.Mod.Path == modulePath {
			return getModule(m.Mod, logger)
		}
	}
	return module.Version{}, "", fmt.Errorf("could not find %s module", modulePath)
}

// getModule will retrieve or copy the module sources to the go build cache.
func getModule(ver module.Version, logger *zap.Logger) (module.Version, string, error) {
	if strings.HasPrefix(ver.Path, "/") || strings.HasPrefix(ver.Path, ".") {
		// We are dealing with a filepath meaning we are building from the filesystem.
		// If this is the case, this is the same as building from the main module.
		// We fill out the version using any git version data and return as-is.
		logger.Info("Module path references the filesystem")
		v, err := getVersion(ver.Path)
		if err != nil {
			return module.Version{}, "", err
		}
		abspath, err := filepath.Abs(ver.Path)
		if err != nil {
			return module.Version{}, "", err
		}
		return module.Version{Version: v}, abspath, nil
	}

	// This references a module. Use go mod download to download the module.
	// We use go mod download specifically to avoid downloading extra dependencies.
	// This should work properly even if vendor was used for the dependencies.
	return downloadModule(ver, logger)
}

// downloadModule will download the module to a file path.
func downloadModule(ver module.Version, logger *zap.Logger) (module.Version, string, error) {
	// Download the module and send the JSON output to stdout.
	cmd := exec.Command("go", "mod", "download", "-json", ver.Path)
	data, err := cmd.Output()
	if err != nil {
		return module.Version{}, "", err
	}

	// Download succeeded. Deserialize the JSON to find the file path.
	var m struct {
		Dir     string
		Path    string
		Version string
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return module.Version{}, "", err
	}
	return module.Version{Path: m.Path, Version: m.Version}, m.Dir, nil
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
	return "v" + v.String(), nil
}

func getGoCache() (string, error) {
	if cacheDir := os.Getenv("GOCACHE"); cacheDir != "" {
		return cacheDir, nil
	}

	cmd := exec.Command("go", "env", "GOCACHE")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
