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

package app

import (
	"context"
	"fmt"
	"os"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/helpers"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/hypernode/cmd/hypernode-controller/app/options"
	hnctrl "volcano.sh/hypernode/pkg/hypernode"
	"volcano.sh/hypernode/pkg/signals"
)

// Run starts the standalone HyperNode controller with optional leader election.
func Run(opt *options.ServerOption) error {
	config, err := buildRESTConfig(opt)
	if err != nil {
		return err
	}

	if opt.EnableHealthz {
		if err := helpers.StartHealthz(opt.HealthzBindAddress, "vc-hypernode-controller", opt.CaCertData, opt.CertData, opt.KeyData); err != nil {
			return err
		}
	}

	run := startHyperNodeController(config)
	ctx := signals.SetupSignalContext()

	if !opt.LeaderElection.LeaderElect {
		run(ctx)
		return fmt.Errorf("finished without leader elect")
	}

	leaderElectionClient, err := kubeclientset.NewForConfig(rest.AddUserAgent(config, "hypernode-leader-election"))
	if err != nil {
		return err
	}

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: leaderElectionClient.CoreV1().Events(opt.LeaderElection.ResourceNamespace)})
	eventRecorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "vc-hypernode-controller"})

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %v", err)
	}
	id := hostname + "_" + string(uuid.NewUUID())

	if len(opt.LockObjectNamespace) > 0 {
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

func buildRESTConfig(opt *options.ServerOption) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opt.KubeClientOptions.KubeConfig != "" {
		loadingRules.ExplicitPath = opt.KubeClientOptions.KubeConfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if opt.KubeClientOptions.Master != "" {
		overrides.ClusterInfo.Server = opt.KubeClientOptions.Master
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, err
	}
	cfg.QPS = opt.KubeClientOptions.QPS
	cfg.Burst = opt.KubeClientOptions.Burst
	return cfg, nil
}

func startHyperNodeController(config *rest.Config) func(ctx context.Context) {
	kubeClient := kubeclientset.NewForConfigOrDie(config)
	vcClient := vcclientset.NewForConfigOrDie(config)
	kubeInformer := informers.NewSharedInformerFactory(kubeClient, 0)
	vcInformer := informerfactory.NewSharedInformerFactory(vcClient, 0)

	ctrl := hnctrl.NewController()
	if err := ctrl.Initialize(&hnctrl.Options{
		KubeClient:              kubeClient,
		VolcanoClient:           vcClient,
		SharedInformerFactory:   kubeInformer,
		VCSharedInformerFactory: vcInformer,
	}); err != nil {
		klog.Fatalf("failed to initialize hypernode controller: %v", err)
	}

	return func(ctx context.Context) {
		go ctrl.Run(ctx.Done())
		<-ctx.Done()
	}
}
