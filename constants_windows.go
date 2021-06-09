package main

// pkgConfigExecName is the expected "default" name this program will have
// when it is built by `go build`. We use this to find & remove this wrapper
// program from the `PATH`, so we can locate and call the "real" pkg-config.
const pkgConfigExecName = "pkg-config.exe"
