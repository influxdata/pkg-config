package flux

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"go.uber.org/zap"

	"github.com/influxdata/pkg-config/internal/logutil"
)

// ltoSections returns the names of the object file sections that carry
// compiler intermediate representation (LLVM bitcode) intended for
// link-time optimisation, for archives built for the given target OS.
//
// Cargo embeds bitcode in every object it produces when the release
// profile enables LTO, and the Rust sysroot ships compiler_builtins with
// bitcode embedded, so the static library that cargo emits always
// contains these sections. Nothing ever performs LTO on libflux, so the
// sections are dead weight, but Go 1.27.4+ (and 1.26.11+) refuse to pass
// any archive containing them to the system linker, because the linker
// would otherwise hand their contents to an LTO plugin at build time.
func ltoSections(goos string) []string {
	if goos == "darwin" {
		return []string{"__LLVM,__bitcode", "__LLVM,__cmdline"}
	}
	return []string{".llvmbc", ".llvmcmd"}
}

// objcopyEnv is the environment variable that overrides the tool used to
// strip LTO sections from the built archives.
const objcopyEnv = "OBJCOPY"

// findObjcopy locates a tool capable of removing sections from the
// archives produced for the target. It prefers, in order:
//
//  1. The executable named by $OBJCOPY.
//  2. llvm-objcopy from the Rust toolchain's llvm-tools component, which is
//     versioned with the compiler that produced the objects and understands
//     every object format cargo can emit.
//  3. llvm-objcopy on the PATH.
//  4. GNU objcopy on the PATH, which cannot read Mach-O archives and so is
//     only considered for non-darwin targets.
//
// rustc is invoked from dir so that any rust-toolchain file there selects
// the same toolchain cargo used.
func findObjcopy(ctx context.Context, logger *zap.Logger, dir string, target Target) (string, error) {
	if tool := os.Getenv(objcopyEnv); tool != "" {
		logger.Info("Using objcopy from environment", zap.String("tool", tool))
		return tool, nil
	}

	if tool, err := rustLLVMObjcopy(ctx, dir); err != nil {
		logger.Info("Could not locate llvm-objcopy in the Rust toolchain", zap.Error(err))
	} else {
		logger.Info("Using llvm-objcopy from the Rust toolchain", zap.String("tool", tool))
		return tool, nil
	}

	if tool, err := exec.LookPath("llvm-objcopy"); err == nil {
		logger.Info("Using llvm-objcopy from PATH", zap.String("tool", tool))
		return tool, nil
	}

	if target.OS != "darwin" {
		if tool, err := exec.LookPath("objcopy"); err == nil {
			logger.Info("Using objcopy from PATH", zap.String("tool", tool))
			return tool, nil
		}
	}

	return "", fmt.Errorf("could not find a tool to strip LTO sections from the %s archive: "+
		"install the llvm-tools component (rustup component add llvm-tools), "+
		"put llvm-objcopy or objcopy on the PATH, or set $%s", target, objcopyEnv)
}

// rustLLVMObjcopy returns the path to the llvm-objcopy shipped with the
// active Rust toolchain's llvm-tools component, or an error if it is not
// installed.
func rustLLVMObjcopy(ctx context.Context, dir string) (string, error) {
	rustc := os.Getenv("RUSTC")
	if rustc == "" {
		rustc = "rustc"
	}

	sysroot, err := rustcOutput(ctx, rustc, dir, "--print", "sysroot")
	if err != nil {
		return "", err
	}
	host, err := rustcHost(ctx, rustc, dir)
	if err != nil {
		return "", err
	}

	name := "llvm-objcopy"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	tool := filepath.Join(sysroot, "lib", "rustlib", host, "bin", name)
	if _, err := os.Stat(tool); err != nil {
		return "", err
	}
	return tool, nil
}

// rustcHost returns the host target triple of the given rustc. It is
// parsed from the verbose version output as "--print host-tuple" is only
// available from Rust 1.84.
func rustcHost(ctx context.Context, rustc, dir string) (string, error) {
	verbose, err := rustcOutput(ctx, rustc, dir, "-vV")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(verbose, "\n") {
		if host, ok := strings.CutPrefix(strings.TrimSpace(line), "host: "); ok {
			return strings.TrimSpace(host), nil
		}
	}
	return "", fmt.Errorf("could not determine host triple from %s -vV", rustc)
}

func rustcOutput(ctx context.Context, rustc, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, rustc, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", rustc, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// stripLTOSections copies the archive at src to dst with every LTO section
// removed from its members, using the given objcopy-compatible tool.
func stripLTOSections(ctx context.Context, logger *zap.Logger, tool, src, dst string, target Target) error {
	var args []string
	for _, section := range ltoSections(target.OS) {
		args = append(args, "--remove-section", section)
	}
	args = append(args, src, dst)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr

	logger.Info("Stripping LTO sections", zap.String("tool", tool), zap.String("src", src), zap.String("dst", dst))
	if err := cmd.Run(); err != nil {
		logutil.LogOutput(&stderr, logger)
		return fmt.Errorf("%s: %w", tool, err)
	}
	return nil
}
