/*
Copyright 2018 The Kubernetes Authors.
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

package cronjob

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	volcanofake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
)

// A failed status update must reach the caller so the key is requeued. syncErr
// is always nil at that point, because a non-nil one returns earlier.
func TestSyncReturnsStatusUpdateError(t *testing.T) {
	cronJob := createTestCronJob(cjSpec{concurrency: "Allow"})
	controller, _ := setupTestController()
	setupFakeJobClient(controller, nil, nil)
	controller.now = func() time.Time { return fiveMinutesAfterTen() }

	if err := controller.cronJobInformer.Informer().GetIndexer().Add(cronJob); err != nil {
		t.Fatal(err)
	}

	fakeVC, ok := controller.vcClient.(*volcanofake.Clientset)
	if !ok {
		t.Fatalf("vcClient is %T, want the fake clientset", controller.vcClient)
	}
	fakeVC.PrependReactor("update", "cronjobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("status update rejected")
	})

	if _, err := controller.sync(cronJob.Namespace + "/" + cronJob.Name); err == nil {
		t.Fatal("sync returned nil, want the status update error")
	}
}
