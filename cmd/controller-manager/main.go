/*
Copyright 2017 The Volcano Authors.

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

	"k8s.io/apimachinery/pkg/util/wait"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog"

	_ "volcano.sh/volcano/pkg/controllers/garbagecollector"
	_ "volcano.sh/volcano/pkg/controllers/job"
	_ "volcano.sh/volcano/pkg/controllers/podgroup"
	_ "volcano.sh/volcano/pkg/controllers/queue"

	"volcano.sh/volcano/cmd/controller-manager/app"
	"volcano.sh/volcano/cmd/controller-manager/app/options"
	"volcano.sh/volcano/pkg/version"
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	klog.InitFlags(nil)

	s := options.NewServerOption()
	s.AddFlags(pflag.CommandLine)

	cliflag.InitFlags()

	if s.PrintVersion {
		version.PrintVersionAndExit()
	}
	if err := s.CheckOptionOrDie(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	// The default klog flush interval is 30 seconds, which is frighteningly long.
	go wait.Until(klog.Flush, *logFlushFreq, wait.NeverStop)
	defer klog.Flush()

	if err := app.Run(s); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
