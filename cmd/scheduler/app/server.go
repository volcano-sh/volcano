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
	"fmt"
	"net/http"
	"os"

	"volcano.sh/apis/pkg/apis/helpers"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/kube"
	"volcano.sh/volcano/pkg/scheduler"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/signals"
	commonutil "volcano.sh/volcano/pkg/util"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"

	// Register gcp auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"

	// Register rest client metrics
	_ "k8s.io/component-base/metrics/prometheus/restclient"
)

// Run the volcano scheduler.
func Run(opt *options.ServerOption) error {
	config, err := kube.BuildConfig(opt.KubeClientOptions)
	if err != nil {
		return err
	}

	if opt.PluginsDir != "" {
		err := framework.LoadCustomPlugins(opt.PluginsDir)
		if err != nil {
			klog.Errorf("Fail to load custom plugins: %v", err)
			return err
		}
	}

	sched, err := scheduler.NewScheduler(config, opt)
	if err != nil {
		panic(err)
	}

	if opt.EnableMetrics {
		go func() {
			http.Handle("/metrics", promHandler())
			klog.Fatalf("Prometheus Http Server failed %s", http.ListenAndServe(opt.ListenAddress, nil))
		}()
	}

	if opt.EnableHealthz {
		if err := helpers.StartHealthz(opt.HealthzBindAddress, "volcano-scheduler", opt.CaCertData, opt.CertData, opt.KeyData); err != nil {
			return err
		}
	}

	ctx := signals.SetupSignalContext()
	run := func(ctx context.Context) {
		sched.Run(ctx.Done())
		<-ctx.Done()
	}

	if !opt.LeaderElection.LeaderElect {
		run(ctx)
		return fmt.Errorf("finished without leader elect")
	}

	leaderElectionClient, err := clientset.NewForConfig(restclient.AddUserAgent(config, "leader-election"))
	if err != nil {
		return err
	}

	// Prepare event clients.
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: leaderElectionClient.CoreV1().Events(opt.LeaderElection.ResourceNamespace)})
	eventRecorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: commonutil.GenerateComponentName(opt.SchedulerNames)})

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %v", err)
	}
	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id := hostname + "_" + string(uuid.NewUUID())
	// set ResourceNamespace value to LockObjectNamespace when it's not empty,compatible with old flag
	if len(opt.LockObjectNamespace) > 0 {
		opt.LeaderElection.ResourceNamespace = opt.LockObjectNamespace
	}
	rl, err := resourcelock.New(resourcelock.LeasesResourceLock,
		opt.LeaderElection.ResourceNamespace,
		opt.LeaderElection.ResourceName,
		leaderElectionClient.CoreV1(),
		leaderElectionClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: eventRecorder,
		})
	if err != nil {
		return fmt.Errorf("couldn't create resource lock: %v", err)
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: opt.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: opt.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   opt.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				klog.Fatalf("leaderelection lost")
			},
		},
	})
	return fmt.Errorf("lost lease")
}

func promHandler() http.Handler {
	// Unregister go and process related collector because it's duplicated and `legacyregistry.DefaultGatherer` also has registered them.
	prometheus.DefaultRegisterer.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.DefaultRegisterer.Unregister(collectors.NewGoCollector())
	return promhttp.InstrumentMetricHandler(prometheus.DefaultRegisterer, promhttp.HandlerFor(prometheus.Gatherers{prometheus.DefaultGatherer, legacyregistry.DefaultGatherer}, promhttp.HandlerOpts{}))
}
