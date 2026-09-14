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

package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/helpers"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformers "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/cmd/hypernode-controller-manager/app/options"
	"volcano.sh/volcano/pkg/controllers/framework"
	"volcano.sh/volcano/pkg/controllers/hypernode"
	"volcano.sh/volcano/pkg/kube"
	"volcano.sh/volcano/pkg/signals"
	commonutil "volcano.sh/volcano/pkg/util"
)

const componentName = "vc-hypernode-controller-manager"

var processReady atomic.Bool

// Run starts the standalone HyperNode controller manager.
func Run(opt *options.ServerOption) error {
	if opt == nil {
		return errors.New("server options must not be nil")
	}
	processReady.Store(false)
	defer processReady.Store(false)

	config, err := kube.BuildConfig(opt.KubeClientOptions)
	if err != nil {
		return err
	}
	if opt.EnableHealthz {
		if err := startHealthz(opt.HealthzAddress); err != nil {
			return err
		}
	}
	if opt.EnableMetrics {
		go serveMetrics(opt.ListenAddress)
	}

	run, err := prepareController(config)
	if err != nil {
		return err
	}
	ctx := signals.SetupSignalContext()
	if !opt.LeaderElection.LeaderElect {
		return run(ctx)
	}

	leaderElectionClient, err := kubeclientset.NewForConfig(rest.AddUserAgent(config, "hypernode-leader-election"))
	if err != nil {
		return err
	}
	broadcaster := record.NewBroadcaster()
	defer broadcaster.Shutdown()
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: leaderElectionClient.CoreV1().Events(opt.LeaderElection.ResourceNamespace)})
	eventRecorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: componentName})

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %w", err)
	}
	identity := hostname + "_" + string(uuid.NewUUID())
	lock, err := resourcelock.New(
		opt.LeaderElection.ResourceLock,
		opt.LeaderElection.ResourceNamespace,
		opt.LeaderElection.ResourceName,
		leaderElectionClient.CoreV1(),
		leaderElectionClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{Identity: identity, EventRecorder: eventRecorder},
	)
	if err != nil {
		return fmt.Errorf("couldn't create resource lock: %w", err)
	}

	controllerResult := make(chan error, 1)
	var startedLeading atomic.Bool
	electionCtx, cancelElection := context.WithCancel(ctx)
	defer cancelElection()
	leaderElector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: lock, LeaseDuration: opt.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: opt.LeaderElection.RenewDeadline.Duration, RetryPeriod: opt.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				startedLeading.Store(true)
				// A promoted follower must not remain ready while its controller
				// caches are still starting.
				processReady.Store(false)
				err := run(leaderCtx)
				processReady.Store(false)
				controllerResult <- err
				if err != nil {
					// Stop leader election when the controller exits unexpectedly.
					cancelElection()
				}
			},
			OnStoppedLeading: func() {
				processReady.Store(false)
			},
			OnNewLeader: func(leaderIdentity string) {
				if leaderIdentity == identity {
					klog.InfoS("Acquired HyperNode controller leadership")
					return
				}
				// Observing another leader proves that this follower can access the
				// Lease. Followers remain ready so a rolling update does not wait for
				// the current leader to terminate before progressing.
				processReady.Store(true)
				klog.InfoS("Observed HyperNode controller leader", "identity", leaderIdentity)
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create leader elector: %w", err)
	}
	leaderElector.Run(electionCtx)
	if startedLeading.Load() || leaderElector.IsLeader() {
		// OnStartedLeading is asynchronous. Wait until controller shutdown has
		// completed before allowing the process to exit.
		if err := <-controllerResult; err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("lost lease")
}

func prepareController(config *rest.Config) (func(context.Context) error, error) {
	if config == nil {
		return nil, errors.New("REST config must not be nil")
	}
	kubeClient, err := kubeclientset.NewForConfig(rest.AddUserAgent(config, componentName))
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	volcanoClient, err := vcclientset.NewForConfig(rest.AddUserAgent(config, componentName))
	if err != nil {
		return nil, fmt.Errorf("failed to create Volcano client: %w", err)
	}
	controllerOptions := &framework.ControllerOption{
		KubeClient: kubeClient, VolcanoClient: volcanoClient, Config: config,
		SharedInformerFactory:   informers.NewSharedInformerFactory(kubeClient, 0),
		VCSharedInformerFactory: vcinformers.NewSharedInformerFactory(volcanoClient, 0),
	}

	controller := hypernode.NewController()
	if err := controller.Initialize(controllerOptions); err != nil {
		return nil, fmt.Errorf("failed to initialize HyperNode controller: %w", err)
	}

	return func(ctx context.Context) error {
		controllerDone := make(chan struct{})
		go func() {
			defer close(controllerDone)
			controller.Run(ctx.Done())
		}()
		select {
		case <-controller.CacheSyncSucceeded():
			processReady.Store(true)
			select {
			case <-ctx.Done():
				// Wait for informer, queue, and discoverer shutdown before exiting.
				<-controllerDone
			case <-controllerDone:
				if ctx.Err() == nil {
					processReady.Store(false)
					return errors.New("HyperNode controller stopped unexpectedly")
				}
			}
		case <-ctx.Done():
			// Wait for informer, queue, and discoverer shutdown before exiting.
			<-controllerDone
		case <-controllerDone:
			if ctx.Err() == nil {
				processReady.Store(false)
				return errors.New("HyperNode controller stopped unexpectedly")
			}
		}
		return nil
	}, nil
}

func healthzHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !processReady.Load() {
			http.Error(w, "HyperNode controller is not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func startHealthz(address string) error {
	server := &http.Server{Addr: address, Handler: healthzHandler(), ReadHeaderTimeout: helpers.DefaultReadHeaderTimeout, ReadTimeout: helpers.DefaultReadTimeout, WriteTimeout: helpers.DefaultWriteTimeout}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to create health listener: %w", err)
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			klog.Fatalf("Health HTTP server failed: %s", err)
		}
	}()
	return nil
}

func serveMetrics(address string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", commonutil.PromHandler())
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: helpers.DefaultReadHeaderTimeout, ReadTimeout: helpers.DefaultReadTimeout, WriteTimeout: helpers.DefaultWriteTimeout}
	klog.Fatalf("Prometheus HTTP server failed: %s", server.ListenAndServe())
}
