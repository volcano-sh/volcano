/*
Copyright 2018 The Volcano Authors.

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
package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/spf13/pflag"
	_ "go.uber.org/automaxprocs"

	"k8s.io/apimachinery/pkg/util/wait"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog"

	"volcano.sh/volcano/cmd/webhook-manager/app"
	"volcano.sh/volcano/cmd/webhook-manager/app/options"

	_ "volcano.sh/volcano/pkg/webhooks/admission/jobs/mutate"
	_ "volcano.sh/volcano/pkg/webhooks/admission/jobs/validate"
	_ "volcano.sh/volcano/pkg/webhooks/admission/podgroups/mutate"
	_ "volcano.sh/volcano/pkg/webhooks/admission/pods/mutate"
	_ "volcano.sh/volcano/pkg/webhooks/admission/pods/validate"
	_ "volcano.sh/volcano/pkg/webhooks/admission/queues/mutate"
	_ "volcano.sh/volcano/pkg/webhooks/admission/queues/validate"
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	klog.InitFlags(nil)

	config := options.NewConfig()
	config.AddFlags(pflag.CommandLine)

	cliflag.InitFlags()

	go wait.Until(klog.Flush, *logFlushFreq, wait.NeverStop)
	defer klog.Flush()

	if err := config.CheckPortOrDie(); err != nil {
		klog.Fatalf("Configured port is invalid: %v", err)
	}

	if err := config.ParseCAFiles(nil); err != nil {
		klog.Fatalf("Failed to parse CA file: %v", err)
	}

	if err := app.Run(config); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
