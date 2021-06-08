package flux

import "regexp"

var fluxVersionRegexp = regexp.MustCompile(`\\github\.com\\influxdata\\flux@(v\d+\.\d+\.\d+.*)$`)

const pcSep = "\\\\"
