package vqa

import (
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"reflect"
	"time"
	"volcano.sh/apis/pkg/apis/autoscaling/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

func (v *vqacontroller) addVQA(obj interface{}) {
	vqa, ok := obj.(*v1alpha1.VerticalQueueAutoscaler)
	if !ok {
		klog.Errorf("Failed to convert %v to v1.Pod", obj)
		return
	}
	req := &apis.Request{
		VQAName:      vqa.Name,
		QueueName:    vqa.Spec.Queue,
		VQARetryTime: 0,
	}
	v.enqueue(req)
}

func (v *vqacontroller) updateVQA(oldObj interface{}, newObj interface{}) {
	oldVQA, ok := oldObj.(*v1alpha1.VerticalQueueAutoscaler)
	if !ok {
		klog.Errorf("Failed to convert %v to v1alpha1.VerticalQueueAutoscaler", oldVQA)
		return
	}
	newVQA, ok := newObj.(*v1alpha1.VerticalQueueAutoscaler)
	if !ok {
		klog.Errorf("Failed to convert %v to v1alpha1.VerticalQueueAutoscaler", newVQA)
		return
	}

	// No need to update if ResourceVersion is not changed
	if oldVQA.ResourceVersion == newVQA.ResourceVersion {
		klog.V(6).Infof("No need to update because VerticalQueueAutoscaler is not modified.")
		return
	}

	// NOTE: Since we only reconcile vqa based on Spec, we will ignore other attributes
	// For vqa status, it's used internally and always been updated via our controller.
	if reflect.DeepEqual(oldVQA.Spec, newVQA.Spec) {
		klog.V(6).Infof("VQA update event is ignored since no update in 'Spec'.")
		return
	}

	req := &apis.Request{
		VQAName:      newVQA.Name,
		QueueName:    newVQA.Spec.Queue,
		VQARetryTime: 0,
	}
	v.enqueue(req)
}

func (v *vqacontroller) deleteVQA(obj interface{}) {
	vqa, ok := obj.(*v1alpha1.VerticalQueueAutoscaler)
	if !ok {
		klog.Errorf("Failed to convert %v to v1alpha1.VerticalQueueAutoscaler", obj)
		return
	}
	req := &apis.Request{
		VQAName:   vqa.Name,
		QueueName: vqa.Spec.Queue,
	}
	v.tidalQueue.Done(req)
	metrics.DeleteVqaMetrics(vqa.Name, vqa.Labels[metrics.TenantKey])
}

func (v *vqacontroller) enqueue(obj *apis.Request) {
	v.tidalQueue.Add(obj)
}

func (v *vqacontroller) enqueueAfter(obj *apis.Request, after time.Duration) {
	v.tidalQueue.AddAfter(obj, after)
}

func (v *vqacontroller) updateStatus(vqa *v1alpha1.VerticalQueueAutoscaler) {
	retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if _, err := v.vcClient.AutoscalingV1alpha1().VerticalQueueAutoscalers().Get(context.TODO(), vqa.Name, metav1.GetOptions{}); err != nil {
			klog.V(0).Info("update vqa status , but vqa not found!", "vqa", vqa.Name, "err", err)
			return err
		}
		if _, err := v.vcClient.AutoscalingV1alpha1().VerticalQueueAutoscalers().UpdateStatus(context.TODO(), vqa, metav1.UpdateOptions{}); err != nil {
			if !apierrors.IsConflict(err) {
				klog.V(0).ErrorS(err, "update vqa status failed!", "vqa", vqa.Name)
			}
			if err != nil {
				klog.V(0).ErrorS(err, "update vqa status conflict!", "vqa", vqa.Name)
			}
			return err
		}
		klog.V(2).Info("update vqa status success!", "vqa", vqa.Name)
		return nil
	})
}
