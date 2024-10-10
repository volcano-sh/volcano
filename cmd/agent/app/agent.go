/*
Copyright 2024 The Volcano Authors.

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

package app

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/agent/app/options"
	"volcano.sh/volcano/pkg/agent/events"
	"volcano.sh/volcano/pkg/agent/healthcheck"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/metriccollect"
	"volcano.sh/volcano/pkg/networkqos"
)

func NewVolcanoAgentCommand(ctx context.Context) *cobra.Command {
	opts := options.NewVolcanoAgentOptions()
	cmd := &cobra.Command{
		Use:  "volcano-agent",
		Long: `volcano-agent.`,
		Run: func(cmd *cobra.Command, args []string) {
			cliflag.PrintFlags(cmd.Flags())
			if err := Run(ctx, opts); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
		},
	}

	opts.AddFlags(cmd)
	globalflag.AddGlobalFlags(cmd.Flags(), cmd.Name())
	return cmd
}

func Run(ctx context.Context, opts *options.VolcanoAgentOptions) error {
	klog.InfoS("Start volcano agent")
	conf, err := NewConfiguration(opts)
	if err != nil {
		return fmt.Errorf("failed to create volcano-agent configuration: %v", err)
	}

	// TODO: get cgroup driver dynamically
	sfsFsPath := strings.TrimSpace(os.Getenv(utils.SysFsPathEnv))
	if sfsFsPath == "" {
		sfsFsPath = utils.DefaultSysFsPath
	}
	cgroupManager := cgroup.NewCgroupManager("cgroupfs", path.Join(sfsFsPath, "cgroup"), conf.GenericConfiguration.KubeCgroupRoot)
	metricCollectorManager, err := metriccollect.NewMetricCollectorManager(conf, cgroupManager)
	if err != nil {
		return fmt.Errorf("failed to create metric collector manager: %v", err)
	}

	networkQoSMgr := networkqos.NewNetworkQoSManager(conf)
	err = networkQoSMgr.Init()
	if err != nil {
		return fmt.Errorf("failed to init network qos: %v", err)
	}

	eventManager := events.NewEventManager(conf, metricCollectorManager, cgroupManager)
	err = eventManager.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to run event manager: %v", err)
	}

	conf.InformerFactory.K8SInformerFactory.Start(ctx.Done())
	RunServer(healthcheck.NewHealthChecker(networkQoSMgr), conf.GenericConfiguration.HealthzAddress, conf.GenericConfiguration.HealthzPort)
	klog.InfoS("Volcano volcano-agent started")
	<-ctx.Done()
	klog.InfoS("Volcano volcano-agent stopped")
	return nil
}
