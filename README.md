# Go Pkg-Config

This binary is intended as an easier way to include C code in your Go program.

This binary can be installed and Go can be told to use this binary when it invokes `pkg-config`.
If it finds a library that is known by the program, it will compile and output the location for that binary.
If it doesn't know what the program is, it will default to invoking the system `pkg-config` to obtain the compilation flags.

## Requirements

Building `flux` requires `cargo` and a tool that can remove sections from
the static library that cargo produces. The archive that cargo emits
contains LLVM bitcode (`.llvmbc`/`.llvmcmd` sections, or the `__LLVM`
segment on macOS) in most of its members, both because cargo embeds
bitcode when the release profile enables LTO and because the Rust sysroot
ships `compiler_builtins` with bitcode embedded. Go 1.27.4 and 1.26.11
onwards refuse to pass any archive containing such sections to the
system linker, so they are stripped before the library is installed.

The following tools are tried, in order:

1. The executable named by the `OBJCOPY` environment variable.
2. `llvm-objcopy` from the Rust toolchain's `llvm-tools` component
   (`rustup component add llvm-tools`). This is the only option on macOS
   that does not require installing anything else.
3. `llvm-objcopy` on the `PATH`.
4. GNU `objcopy` on the `PATH` (not used for macOS targets, as it cannot
   read Mach-O archives).
