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

package source

import (
	"time"

	"k8s.io/client-go/util/workqueue"

	"volcano.sh/volcano/pkg/agent/config/api"
)

type fakeSource struct {
	queue workqueue.RateLimitingInterface
	cfg   *api.ColocationConfig
}

func NewFakeSource(queue workqueue.RateLimitingInterface) *fakeSource {
	return &fakeSource{
		queue: queue,
	}
}

func (f *fakeSource) Source(stopCh <-chan struct{}) (change workqueue.RateLimitingInterface, err error) {
	return f.queue, nil
}

func (f *fakeSource) GetLatestConfig() (cfg *api.ColocationConfig, err error) {
	return f.cfg, nil
}

func (f *fakeSource) Stop() {
	for {
		if f.queue.Len() == 0 {
			f.queue.ShutDown()
			return
		}
		time.Sleep(1 * time.Second)
	}
}
