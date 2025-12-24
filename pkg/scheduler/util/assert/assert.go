/*
Copyright 2019 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package assert

import (
	"fmt"
	"os"
	"runtime/debug"

	"k8s.io/klog/v2"
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
