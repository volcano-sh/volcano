package assert

import (
	"fmt"
	"os"
	"runtime/debug"

	"k8s.io/klog"
)

const (
	// EnvPanicOnError is the env name to determine panic on assertion failed or not
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

// Assert check condition, if condition is false, print message by log or panic
func Assert(condition bool, message string) {
	if condition {
		return
	}
	if panicOnError {
		panic(message)
	}
	klog.Errorf("%s, %s", message, debug.Stack())
}

// Assertf check condition, if condition is false, print message using Assert
func Assertf(condition bool, format string, args ...interface{}) {
	if condition {
		return
	}
	Assert(condition, fmt.Sprintf(format, args...))
}
