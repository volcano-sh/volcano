package assert

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/golang/glog"
)

const (
	EnvPanicOnError = "PANIC_ON_ERROR"
)

var (
	panicOnError = true
)

func init() {
	env := os.Getenv(EnvPanicOnError)
	if env == "false" {
		panicOnError = false
	}
}

func Assert(condition bool, message string) {
	if condition {
		return
	}
	if panicOnError {
		panic(message)
	}
	glog.Errorf("%s, %s", message, debug.Stack())
}

func Assertf(condition bool, format string, args ...interface{}) {
	if condition {
		return
	}
	Assert(condition, fmt.Sprintf(format, args...))
}
