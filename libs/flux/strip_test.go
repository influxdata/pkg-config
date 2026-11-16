package flux

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.uber.org/zap"
)

func TestLTOSections(t *testing.T) {
	for _, tt := range []struct {
		goos string
		want []string
	}{
		{goos: "linux", want: []string{".llvmbc", ".llvmcmd"}},
		{goos: "windows", want: []string{".llvmbc", ".llvmcmd"}},
		{goos: "darwin", want: []string{"__LLVM,__bitcode", "__LLVM,__cmdline"}},
	} {
		t.Run(tt.goos, func(t *testing.T) {
			got := ltoSections(tt.goos)
			if len(got) != len(tt.want) {
				t.Fatalf("ltoSections(%q) = %v, want %v", tt.goos, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ltoSections(%q) = %v, want %v", tt.goos, got, tt.want)
				}
			}
		})
	}
}

func TestFindObjcopyEnv(t *testing.T) {
	t.Setenv(objcopyEnv, "/nonexistent/my-objcopy")
	tool, err := findObjcopy(context.Background(), zap.NewNop(), t.TempDir(), Target{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if tool != "/nonexistent/my-objcopy" {
		t.Fatalf("findObjcopy = %q, want $%s to take precedence", tool, objcopyEnv)
	}
}

func TestFindObjcopyNotFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH handling differs on windows")
	}
	t.Setenv(objcopyEnv, "")
	// An empty PATH and a rustc that fails ensures no candidate is found.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("RUSTC", "/nonexistent/rustc")
	_, err := findObjcopy(context.Background(), zap.NewNop(), t.TempDir(), Target{OS: "linux", Arch: "amd64"})
	if err == nil {
		t.Fatal("findObjcopy succeeded, want an error when no tool is available")
	}
}

// TestStripLTOSections exercises the real tool, if one is available, on
// an archive from the Rust sysroot that is known to contain bitcode.
func TestStripLTOSections(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}

	tool, err := findObjcopy(ctx, logger, t.TempDir(), target)
	if err != nil {
		t.Skip(err)
	}
	sysroot, err := rustcOutput(ctx, "rustc", t.TempDir(), "--print", "sysroot")
	if err != nil {
		t.Skip(err)
	}
	host, err := rustcHost(ctx, "rustc", t.TempDir())
	if err != nil {
		t.Skip(err)
	}
	matches, _ := filepath.Glob(filepath.Join(sysroot, "lib", "rustlib", host, "lib", "libcompiler_builtins-*.rlib"))
	if len(matches) == 0 {
		t.Skip("no compiler_builtins rlib in sysroot")
	}

	src := matches[0]
	dst := filepath.Join(t.TempDir(), "out.a")
	if err := stripLTOSections(ctx, logger, tool, src, dst, target); err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) == 0 || len(after) >= len(before) {
		t.Fatalf("stripped archive is %d bytes, original is %d bytes; expected it to shrink", len(after), len(before))
	}
}
