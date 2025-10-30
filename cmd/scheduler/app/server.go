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
	"net/http/pprof"
	"os"

	"volcano.sh/apis/pkg/apis/helpers"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/kube"
	"volcano.sh/volcano/pkg/scheduler"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/signals"
	commonutil "volcano.sh/volcano/pkg/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"

	utilfeature "k8s.io/apiserver/pkg/util/feature"

	// Register gcp auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	basecompatibility "k8s.io/component-base/compatibility"

	// Register rest client metrics
	_ "k8s.io/component-base/metrics/prometheus/restclient"
)

// Run the volcano scheduler.
func Run(opt *options.ServerOption) error {
	config, err := kube.BuildConfig(opt.KubeClientOptions)
	if err != nil {
		return err
	}

	// Align default feature-gates with the connected cluster's version.
	if err := setupComponentGlobals(config); err != nil {
		klog.Errorf("failed to set component globals: %v", err)
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

	if opt.EnableMetrics || opt.EnablePprof {
		metrics.InitKubeSchedulerRelatedMetrics()
		go startMetricsServer(opt)
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
	//lint:ignore SA1019 LockObjectNamespace is deprecated and will be removed in a future release
	if len(opt.LockObjectNamespace) > 0 {
		//lint:ignore SA1019 LockObjectNamespace is deprecated and will be removed in a future release
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

func startMetricsServer(opt *options.ServerOption) {
	mux := http.NewServeMux()

	if opt.EnableMetrics {
		mux.Handle("/metrics", commonutil.PromHandler())
	}

	if opt.EnablePprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	server := &http.Server{
		Addr:              opt.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: helpers.DefaultReadHeaderTimeout,
		ReadTimeout:       helpers.DefaultReadTimeout,
		WriteTimeout:      helpers.DefaultWriteTimeout,
	}

	if err := server.ListenAndServe(); err != nil {
		klog.Errorf("start metrics/pprof http server failed: %v", err)
	}
}

// setupComponentGlobals discovers the API server version and sets
// Volcano's effective version + feature gate defaults to match the cluster.
// This makes defaults (like DRA) correct on older clusters.
func setupComponentGlobals(config *restclient.Config) error {
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return err
	}

	serverVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	kubeVersion := fmt.Sprintf("%s.%s", serverVersion.Major, serverVersion.Minor)
	kubeEffectiveVersion := basecompatibility.NewEffectiveVersionFromString(kubeVersion, "", "")

	componentGlobalsRegistry := basecompatibility.NewComponentGlobalsRegistry()
	if componentGlobalsRegistry.EffectiveVersionFor(basecompatibility.DefaultKubeComponent) == nil {
		err = componentGlobalsRegistry.Register(
			basecompatibility.DefaultKubeComponent,
			kubeEffectiveVersion,
			utilfeature.DefaultMutableFeatureGate,
		)
		if err != nil {
			return fmt.Errorf("failed to register component globals: %w", err)
		}
	}

	return componentGlobalsRegistry.Set()
}
