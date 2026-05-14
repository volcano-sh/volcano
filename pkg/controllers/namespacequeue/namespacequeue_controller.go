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

// Package namespacequeue implements the NamespaceQueue controller.
//
// Design: each NamespaceQueue object is reconciled into a cluster-scoped shadow Queue
// (via buildShadowQueue in shadow_queue.go) so the Volcano scheduler can pick it up
// through the existing Queue informer without any scheduler-side changes. The shadow
// Queue name is deterministic (shadowQueueName), enabling idempotent reconciliation.
//
// Pre-codegen note: the controller uses a dynamic informer for NamespaceQueue objects
// because the generated lister/informer for this new type does not exist yet. After
// running `make generate-code`, replace the dynamic informer with the generated one:
//
//	factory.Scheduling().V1beta1().NamespaceQueues()
package namespacequeue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/controllers/framework"
)

const (
	controllerName = "namespacequeue-controller"
	workers        = 2
)

// namespaceQueueGVR is the GroupVersionResource used with the dynamic client.
// It is replaced by the generated typed informer after `make generate-code`.
var namespaceQueueGVR = schema.GroupVersionResource{
	Group:    schedulingv1beta1.GroupName,
	Version:  schedulingv1beta1.GroupVersion,
	Resource: "namespacequeues",
}

func init() {
	// Registers the controller with the volcano controller-manager framework.
	// The manager will call Initialize then Run when it starts.
	// To activate, add a blank import of this package in cmd/controller-manager/main.go.
	if err := framework.RegisterController(&namespaceQueueController{}); err != nil {
		klog.Fatalf("Failed to register %s: %v", controllerName, err)
	}
}

// namespaceQueueController watches NamespaceQueue objects (via dynamic informer,
// pre-codegen) and reconciles each one into a cluster-scoped shadow Queue so that
// the Volcano scheduler can schedule PodGroups against it without modification.
type namespaceQueueController struct {
	vcClient      vcclientset.Interface
	dynamicClient dynamic.Interface

	dynInformerFactory dynamicinformer.DynamicSharedInformerFactory
	nsqSynced          cache.InformerSynced

	queue workqueue.TypedRateLimitingInterface[string]
}

// Name implements framework.Controller.
func (c *namespaceQueueController) Name() string { return controllerName }

// Initialize implements framework.Controller. It wires up informers and the work queue.
func (c *namespaceQueueController) Initialize(opt *framework.ControllerOption) error {
	if opt.Config == nil {
		return fmt.Errorf("%s: ControllerOption.Config is nil; cannot build dynamic client", controllerName)
	}

	dynamicClient, err := dynamic.NewForConfig(opt.Config)
	if err != nil {
		return fmt.Errorf("%s: failed to create dynamic client: %w", controllerName, err)
	}
	c.dynamicClient = dynamicClient
	c.vcClient = opt.VolcanoClient

	// Dynamic informer for NamespaceQueue.
	// TODO(lfx): replace with factory.Scheduling().V1beta1().NamespaceQueues() after
	//            running `make generate-code` to produce the typed lister/informer.
	c.dynInformerFactory = dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	nsqInformer := c.dynInformerFactory.ForResource(namespaceQueueGVR)

	_, err = nsqInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				c.queue.Add(key)
			}
		},
		UpdateFunc: func(_, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			if err == nil {
				c.queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// DeletionHandlingMetaNamespaceKeyFunc handles tombstone objects correctly.
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				c.queue.Add(key)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("%s: failed to add event handler: %w", controllerName, err)
	}

	c.nsqSynced = nsqInformer.Informer().HasSynced
	c.queue = workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)
	return nil
}

// Run implements framework.Controller. It blocks until stopCh is closed.
func (c *namespaceQueueController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting %s", controllerName)
	defer klog.Infof("Shutting down %s", controllerName)

	c.dynInformerFactory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.nsqSynced) {
		klog.Errorf("%s: timed out waiting for caches to sync", controllerName)
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *namespaceQueueController) runWorker() {
	for c.processNextItem() {
	}
}

func (c *namespaceQueueController) processNextItem() bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)

	if err := c.reconcile(key); err != nil {
		klog.Errorf("%s: error reconciling %q: %v", controllerName, key, err)
		c.queue.AddRateLimited(key)
		return true
	}
	c.queue.Forget(key)
	return true
}

// reconcile is the core reconciliation function. For a given namespace/name key it:
//  1. Fetches the NamespaceQueue via the dynamic client.
//  2. If not found, deletes the corresponding shadow Queue (GC path).
//  3. Otherwise, constructs the desired shadow Queue via buildShadowQueue and
//     creates or updates it in the cluster so the scheduler can see it.
//
// The shadow Queue name is derived by shadowQueueName, which is deterministic and
// based on a hash of namespace+name, guaranteeing idempotency.
func (c *namespaceQueueController) reconcile(key string) error {
	ctx := context.TODO()
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid key %q: %w", key, err)
	}

	// 1. Fetch NamespaceQueue via dynamic client.
	unstrObj, err := c.dynamicClient.Resource(namespaceQueueGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// NamespaceQueue was deleted — clean up the shadow Queue.
		return c.deleteShadowQueue(ctx, namespace, name)
	}
	if err != nil {
		return fmt.Errorf("get NamespaceQueue %s: %w", key, err)
	}

	// 2. Convert unstructured → typed NamespaceQueue.
	nsq := &schedulingv1beta1.NamespaceQueue{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstrObj.Object, nsq); err != nil {
		return fmt.Errorf("convert NamespaceQueue %s: %w", key, err)
	}

	// 3. Build the desired shadow Queue from the NamespaceQueue spec.
	desired := buildShadowQueue(nsq)

	// 4. Create or update the shadow Queue.
	existing, err := c.vcClient.SchedulingV1beta1().Queues().Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		klog.V(4).Infof("%s: creating shadow Queue %q for NamespaceQueue %s", controllerName, desired.Name, key)
		if _, err := c.vcClient.SchedulingV1beta1().Queues().Create(ctx, desired, metav1.CreateOptions{
			FieldManager: controllerName,
		}); err != nil {
			return fmt.Errorf("create shadow Queue %q: %w", desired.Name, err)
		}
		return c.updateStatus(ctx, nsq, desired.Name)
	}
	if err != nil {
		return fmt.Errorf("get shadow Queue %q: %w", desired.Name, err)
	}

	// 5. Update only when spec has drifted (e.g. tenant changed Capability).
	if shadowQueueSpecChanged(existing, desired) {
		klog.V(4).Infof("%s: updating shadow Queue %q for NamespaceQueue %s", controllerName, desired.Name, key)
		updated := existing.DeepCopy()
		updated.Spec = desired.Spec
		if _, err := c.vcClient.SchedulingV1beta1().Queues().Update(ctx, updated, metav1.UpdateOptions{
			FieldManager: controllerName,
		}); err != nil {
			return fmt.Errorf("update shadow Queue %q: %w", desired.Name, err)
		}
	}

	return c.updateStatus(ctx, nsq, desired.Name)
}

// deleteShadowQueue removes the cluster-scoped shadow Queue when the NamespaceQueue
// that owns it has been deleted. We cannot use an OwnerReference for automatic GC
// because Kubernetes forbids cross-namespace ownership (namespaced → cluster-scoped).
// Instead, the controller explicitly deletes by the deterministic shadow Queue name.
func (c *namespaceQueueController) deleteShadowQueue(ctx context.Context, namespace, name string) error {
	shadowName := shadowQueueName(namespace, name)
	klog.V(4).Infof("%s: deleting shadow Queue %q (NamespaceQueue %s/%s deleted)", controllerName, shadowName, namespace, name)
	err := c.vcClient.SchedulingV1beta1().Queues().Delete(ctx, shadowName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil // already gone
	}
	return err
}

// updateStatus patches the NamespaceQueue status with the shadow Queue name and
// mirrors the shadow Queue's current state (Open/Closed/Closing) back to the tenant.
// This gives tenants full observability without requiring access to cluster resources.
func (c *namespaceQueueController) updateStatus(ctx context.Context, nsq *schedulingv1beta1.NamespaceQueue, shadowName string) error {
	shadowQueue, err := c.vcClient.SchedulingV1beta1().Queues().Get(ctx, shadowName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	patch := map[string]interface{}{
		"status": map[string]interface{}{
			"shadowQueueName": shadowName,
			"state":           string(shadowQueue.Status.State),
			"pending":         shadowQueue.Status.Pending,
			"running":         shadowQueue.Status.Running,
			"inqueue":         shadowQueue.Status.Inqueue,
			"completed":       shadowQueue.Status.Completed,
			"unknown":         shadowQueue.Status.Unknown,
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	_, err = c.dynamicClient.Resource(namespaceQueueGVR).Namespace(nsq.Namespace).
		Patch(ctx, nsq.Name, "application/merge-patch+json", patchBytes,
			metav1.PatchOptions{FieldManager: controllerName}, "status")
	return err
}

// OrphanedShadowQueues returns shadow Queues whose owning NamespaceQueue no longer
// exists. This is a diagnostic helper; the reconciler's delete path handles normal GC.
// Call this from a maintenance routine if orphans accumulate (e.g., after a controller restart).
func (c *namespaceQueueController) OrphanedShadowQueues(ctx context.Context) ([]*schedulingv1beta1.Queue, error) {
	allShadow, err := c.vcClient.SchedulingV1beta1().Queues().List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{ManagedByLabelKey: ManagedByLabelVal}).String(),
	})
	if err != nil {
		return nil, err
	}

	var orphans []*schedulingv1beta1.Queue
	for i := range allShadow.Items {
		q := &allShadow.Items[i]
		ref, ok := q.Annotations[NSQRefAnnotationKey]
		if !ok {
			continue
		}
		// ref = "namespace/name"
		ns, name, err := cache.SplitMetaNamespaceKey(ref)
		if err != nil {
			continue
		}
		_, err = c.dynamicClient.Resource(namespaceQueueGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			orphans = append(orphans, q)
		}
	}
	return orphans, nil
}
