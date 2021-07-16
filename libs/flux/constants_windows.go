package flux

import "regexp"

// fluxVersionRegexp is used to extract the version of flux pulled down by `go mod` by inspecting
// its path on the filesystem.
var fluxVersionRegexp = regexp.MustCompile(`\\github\.com\\influxdata\\flux@(v\d+\.\d+\.\d+.*)$`)

// pcSep is the separator used between components in all the path-fields written into `flux.pc`
// by our `pkg-config` wrapper. On Windows we have to double-escape the OS's path separator because:
//   1. The 1st escape is "used" when `pkg-config` prints the paths to the caller (some piece of `go build`)
//   2. The 2nd escape is "used" when `go build` passes the paths to `ld`
// With only a single-escape of the path separator, `go build` ends up passing args like `-Lmypathtoflux`
// instead of `-Lmy\path\to\flux`, and linking fails.
const pcSep = "\\\\"
