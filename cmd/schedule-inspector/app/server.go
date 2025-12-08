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

package app

import (
	"context"

	"k8s.io/klog/v2"

	// Register gcp auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	// Register rest client metrics
	_ "k8s.io/component-base/metrics/prometheus/restclient"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/kube"
	"volcano.sh/volcano/pkg/scheduler"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/signals"
)

// Run the volcano scheduler.
func Run(opt *options.ServerOption) error {
	config, err := kube.BuildConfig(opt.KubeClientOptions)
	if err != nil {
		return err
	}

	if opt.PluginsDir != "" {
		err = framework.LoadCustomPlugins(opt.PluginsDir)
	}
	if err != nil {
		klog.Errorf("Fail to load custom plugins: %v", err)
		return err
	}

	sched, err := scheduler.NewScheduleInspector(config, opt)
	if err != nil {
		panic(err)
	}

	ctx := signals.SetupSignalContext()
	run := func(ctx context.Context) {
		sched.Run(ctx.Done())
		<-ctx.Done()
	}

	run(ctx)

	return nil
}
