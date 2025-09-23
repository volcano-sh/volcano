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

package app

import (
	"context"
	"fmt"
	"net/http"
	"os"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/helpers"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/cmd/controller-manager/app/options"
	"volcano.sh/volcano/pkg/controllers/framework"
	"volcano.sh/volcano/pkg/kube"
	"volcano.sh/volcano/pkg/signals"
	commonutil "volcano.sh/volcano/pkg/util"
)

// Run the controller.
func Run(opt *options.ServerOption) error {
	config, err := kube.BuildConfig(opt.KubeClientOptions)
	if err != nil {
		return err
	}
	if opt.EnableHealthz {
		if err := helpers.StartHealthz(opt.HealthzBindAddress, "volcano-controller", opt.CaCertData, opt.CertData, opt.KeyData); err != nil {
			return err
		}
	}

	if opt.EnableMetrics {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", commonutil.PromHandler())

			server := &http.Server{
				Addr:              opt.ListenAddress,
				Handler:           mux,
				ReadHeaderTimeout: helpers.DefaultReadHeaderTimeout,
				ReadTimeout:       helpers.DefaultReadTimeout,
				WriteTimeout:      helpers.DefaultWriteTimeout,
			}
			klog.Fatalf("Prometheus Http Server failed: %s", server.ListenAndServe())
		}()
	}

	run := startControllers(config, opt)

	ctx := signals.SetupSignalContext()

	if !opt.LeaderElection.LeaderElect {
		run(ctx)
		return fmt.Errorf("finished without leader elect")
	}

	leaderElectionClient, err := kubeclientset.NewForConfig(rest.AddUserAgent(config, "leader-election"))
	if err != nil {
		return err
	}

	// Prepare event clients.
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: leaderElectionClient.CoreV1().Events(opt.LeaderElection.ResourceNamespace)})
	eventRecorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "vc-controller-manager"})

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
	rl, err := resourcelock.New(opt.LeaderElection.ResourceLock,
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

func startControllers(config *rest.Config, opt *options.ServerOption) func(ctx context.Context) {
	controllerOpt := &framework.ControllerOption{}

	controllerOpt.SchedulerNames = opt.SchedulerNames
	controllerOpt.WorkerNum = opt.WorkerThreads
	controllerOpt.CronJobWorkerNum = opt.WorkerThreadsForCronJob
	controllerOpt.MaxRequeueNum = opt.MaxRequeueNum

	// TODO: add user agent for different controllers
	controllerOpt.KubeClient = kubeclientset.NewForConfigOrDie(config)
	controllerOpt.VolcanoClient = vcclientset.NewForConfigOrDie(config)
	controllerOpt.SharedInformerFactory = informers.NewSharedInformerFactory(controllerOpt.KubeClient, 0)
	controllerOpt.VCSharedInformerFactory = informerfactory.NewSharedInformerFactory(controllerOpt.VolcanoClient, 0)
	controllerOpt.InheritOwnerAnnotations = opt.InheritOwnerAnnotations
	controllerOpt.WorkerThreadsForPG = opt.WorkerThreadsForPG
	controllerOpt.WorkerThreadsForQueue = opt.WorkerThreadsForQueue
	controllerOpt.WorkerThreadsForGC = opt.WorkerThreadsForGC
	controllerOpt.Config = config

	return func(ctx context.Context) {
		framework.ForeachController(func(c framework.Controller) {
			// if controller is not enabled, skip it
			if !isControllerEnabled(c.Name(), opt.Controllers) {
				klog.Infof("Controller <%s> is not enable", c.Name())
				return
			}
			if err := c.Initialize(controllerOpt); err != nil {
				klog.Errorf("Failed to initialize controller <%s>: %v", c.Name(), err)
				return
			}

			go c.Run(ctx.Done())
		})

		<-ctx.Done()
	}
}

// isControllerEnabled check if a specified controller enabled or not.
// If the input controllers starts with a "+name" or "name", it is considered as an explicit inclusion.
// Otherwise, it is considered as an explicit exclusion.
func isControllerEnabled(name string, controllers []string) bool {
	hasStar := false
	// if no explicit inclusion or exclusion, enable all controllers by default
	if len(controllers) == 0 {
		return true
	}
	for _, ctrl := range controllers {
		// if we get here, there was an explicit inclusion
		if ctrl == name {
			return true
		}
		// if we get here, there was an explicit inclusion
		if ctrl == "+"+name {
			return true
		}
		// if we get here, there was an explicit exclusion
		if ctrl == "-"+name {
			return false
		}
		if ctrl == "*" {
			hasStar = true
		}
	}
	// if we get here, there was no explicit inclusion or exclusion
	return hasStar
}
