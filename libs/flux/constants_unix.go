//+build !windows

package flux

import "regexp"

// fluxVersionRegexp is used to extract the version of flux pulled down by `go mod` by inspecting
// its path on the filesystem.
var fluxVersionRegexp = regexp.MustCompile(`/github\.com/influxdata/flux@(v\d+\.\d+\.\d+.*)$`)

// pcSep is the separator used between components in all the path-fields written into `flux.pc`
// by our `pkg-config` wrapper. On Unix, the standard path separator works without problems.
const pcSep = "/"
