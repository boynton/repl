package repl
import ("syscall")

var getTermios = syscall.TIOCGETA
var setTermios = syscall.TIOCSETA
