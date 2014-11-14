package repl

import (
	"syscall"
)

var getTermios = syscall.TCGETS
var setTermios = syscall.TCSETS
