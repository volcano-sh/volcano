/*
Copyright 2026 The Volcano Authors.

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
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
	"go.uber.org/automaxprocs/maxprocs"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	cliflag "k8s.io/component-base/cli/flag"
	componentbaseoptions "k8s.io/component-base/config/options"
	_ "k8s.io/component-base/metrics/prometheus/restclient"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/hypernode-controller-manager/app"
	"volcano.sh/volcano/cmd/hypernode-controller-manager/app/options"
	commonutil "volcano.sh/volcano/pkg/util"
	"volcano.sh/volcano/pkg/version"
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func main() {
	klog.InitFlags(nil)
	flag.Set("legacy_stderr_threshold_behavior", "false") //nolint:errcheck
	flag.Set("stderrthreshold", "INFO")                   //nolint:errcheck

	serverOptions := options.NewServerOption()
	serverOptions.AddFlags(pflag.CommandLine)
	commonutil.LeaderElectionDefault(&serverOptions.LeaderElection)
	serverOptions.LeaderElection.ResourceName = "vc-hypernode-controller-manager"
	componentbaseoptions.BindLeaderElectionFlags(&serverOptions.LeaderElection, pflag.CommandLine)
	cliflag.InitFlags()

	if _, err := maxprocs.Set(maxprocs.Logger(klog.Infof)); err != nil {
		klog.Errorf("Failed to set GOMAXPROCS: %v", err)
	}
	if serverOptions.PrintVersion {
		version.PrintVersionAndExit()
		return
	}
	if err := serverOptions.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	klog.StartFlushDaemon(*logFlushFreq)
	defer klog.Flush()
	if err := app.Run(serverOptions); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
