/*
Copyright 2022 The Kubernetes Authors.

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

package descheduler

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/util/uuid"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/klog/v2"
)

// NewLeaderElection starts the leader election code loop
func NewLeaderElection(
	run func() error,
	client clientset.Interface,
	LeaderElectionConfig *componentbaseconfig.LeaderElectionConfiguration,
	ctx context.Context,
) error {
	var id string

	if hostname, err := os.Hostname(); err != nil {
		// on errors, make sure we're unique
		id = string(uuid.NewUUID())
	} else {
		// add a uniquifier so that two processes on the same host don't accidentally both become active
		id = hostname + "_" + string(uuid.NewUUID())
	}

	klog.V(3).Infof("Assigned unique lease holder id: %s", id)

	if len(LeaderElectionConfig.ResourceNamespace) == 0 {
		return fmt.Errorf("namespace may not be empty")
	}

	if len(LeaderElectionConfig.ResourceName) == 0 {
		return fmt.Errorf("name may not be empty")
	}

	lock, err := resourcelock.New(
		LeaderElectionConfig.ResourceLock,
		LeaderElectionConfig.ResourceNamespace,
		LeaderElectionConfig.ResourceName,
		client.CoreV1(),
		client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: id,
		},
	)
	if err != nil {
		return fmt.Errorf("unable to create leader election lock: %v", err)
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   LeaderElectionConfig.LeaseDuration.Duration,
		RenewDeadline:   LeaderElectionConfig.RenewDeadline.Duration,
		RetryPeriod:     LeaderElectionConfig.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				klog.V(1).InfoS("Started leading")
				err := run()
				if err != nil {
					klog.Error(err)
				}
			},
			OnStoppedLeading: func() {
				klog.V(1).InfoS("Leader lost")
			},
			OnNewLeader: func(identity string) {
				// Just got the lock
				if identity == id {
					return
				}
				klog.V(1).Infof("New leader elected: %v", identity)
			},
		},
	})
	return nil
}
