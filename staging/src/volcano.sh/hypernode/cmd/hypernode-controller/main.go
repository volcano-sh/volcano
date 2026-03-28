/*
Copyright 2025 The Volcano Authors.

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
	"time"

	"github.com/spf13/pflag"
	_ "go.uber.org/automaxprocs"
	cliflag "k8s.io/component-base/cli/flag"
	componentbaseoptions "k8s.io/component-base/config/options"
	"k8s.io/klog/v2"

	"volcano.sh/hypernode/cmd/hypernode-controller/app"
	"volcano.sh/hypernode/cmd/hypernode-controller/app/options"
)

var logFlushFreq = pflag.Duration("log-flush-frequency", 5*time.Second, "Maximum number of seconds between log flushes")

func main() {
	klog.InitFlags(nil)

	fs := pflag.CommandLine
	s := options.NewServerOption()
	s.AddFlags(fs)
	componentbaseoptions.BindLeaderElectionFlags(&s.LeaderElection, fs)

	cliflag.InitFlags()

	if err := s.CheckOptionOrDie(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if s.PrintVersion {
		fmt.Println("vc-hypernode-controller (volcano.sh/hypernode)")
		os.Exit(0)
	}

	if s.CaCertFile != "" && s.CertFile != "" && s.KeyFile != "" {
		if err := s.ReadCAFiles(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read TLS files: %v\n", err)
			os.Exit(1)
		}
	}

	klog.StartFlushDaemon(*logFlushFreq)
	defer klog.Flush()

	if err := app.Run(s); err != nil {
		klog.InfoS("HyperNode controller exited", "err", err)
	}
}
