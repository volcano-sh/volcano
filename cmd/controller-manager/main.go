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
	"sort"
	"time"

	"github.com/spf13/pflag"
	_ "go.uber.org/automaxprocs"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	cliflag "k8s.io/component-base/cli/flag"
	componentbaseoptions "k8s.io/component-base/config/options"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/controller-manager/app"
	"volcano.sh/volcano/cmd/controller-manager/app/options"
	"volcano.sh/volcano/pkg/controllers/framework"
	_ "volcano.sh/volcano/pkg/controllers/garbagecollector"
	_ "volcano.sh/volcano/pkg/controllers/hypernode"
	_ "volcano.sh/volcano/pkg/controllers/job"
	_ "volcano.sh/volcano/pkg/controllers/jobflow"
	_ "volcano.sh/volcano/pkg/controllers/jobtemplate"
	_ "volcano.sh/volcano/pkg/controllers/podgroup"
	_ "volcano.sh/volcano/pkg/controllers/queue"
	commonutil "volcano.sh/volcano/pkg/util"
	"volcano.sh/volcano/pkg/version"
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	klog.InitFlags(nil)

	fs := pflag.CommandLine
	s := options.NewServerOption()
	// knownControllers is a list of all known controllers.
	var knownControllers = func() []string {
		controllerNames := []string{}
		fn := func(controller framework.Controller) {
			controllerNames = append(controllerNames, controller.Name())
		}
		framework.ForeachController(fn)
		sort.Strings(controllerNames)
		return controllerNames
	}
	s.AddFlags(fs, knownControllers())
	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	commonutil.LeaderElectionDefault(&s.LeaderElection)
	s.LeaderElection.ResourceName = "vc-controller-manager"
	componentbaseoptions.BindLeaderElectionFlags(&s.LeaderElection, fs)

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
			fmt.Fprintf(os.Stderr, "Failed to parse CA file: %v\n", err)
			os.Exit(1)
		}
	}

	klog.StartFlushDaemon(*logFlushFreq)
	defer klog.Flush()

	if err := app.Run(s); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
