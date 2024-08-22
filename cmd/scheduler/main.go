/*
Copyright 2017 The Kubernetes Authors.

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
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	cliflag "k8s.io/component-base/cli/flag"
	componentbaseoptions "k8s.io/component-base/config/options"
	"k8s.io/klog/v2"

	// init pprof server
	_ "net/http/pprof"

	"volcano.sh/volcano/cmd/scheduler/app"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	commonutil "volcano.sh/volcano/pkg/util"
	"volcano.sh/volcano/pkg/version"

	// Import default actions/plugins.
	_ "volcano.sh/volcano/pkg/scheduler/actions"
	_ "volcano.sh/volcano/pkg/scheduler/plugins"

	// init assert
	_ "volcano.sh/volcano/pkg/scheduler/util/assert"
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	klog.InitFlags(nil)

	fs := pflag.CommandLine
	s := options.NewServerOption()

	s.AddFlags(fs)
	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	commonutil.LeaderElectionDefault(&s.LeaderElection)
	s.LeaderElection.ResourceName = commonutil.GenerateComponentName(s.SchedulerNames)
	componentbaseoptions.BindLeaderElectionFlags(&s.LeaderElection, fs)
	s.RegisterOptions()

	cliflag.InitFlags()

	if s.PrintVersion {
		version.PrintVersionAndExit()
		return
	}

	if err := s.CheckOptionOrDie(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if s.CaCertFile != "" && s.CertFile != "" && s.KeyFile != "" {
		if err := s.ParseCAFiles(nil); err != nil {
			klog.Fatalf("Failed to parse CA file: %v", err)
		}
	}

	klog.StartFlushDaemon(*logFlushFreq)
	defer klog.Flush()

	if err := app.Run(s); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
