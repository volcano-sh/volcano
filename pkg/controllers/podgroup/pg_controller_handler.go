/*
Copyright 2019 The Volcano Authors.

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

package podgroup

import (
	"context"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/apis/helpers"
	scheduling "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

type podRequest struct {
	podName      string
	podNamespace string
}

func (pg *pgcontroller) addPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("Failed to convert %v to v1.Pod", obj)
		return
	}

	req := podRequest{
		podName:      pod.Name,
		podNamespace: pod.Namespace,
	}

	pg.queue.Add(req)
}

func (pg *pgcontroller) updatePodAnnotations(pod *v1.Pod, pgName string) error {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	if pod.Annotations[scheduling.KubeGroupNameAnnotationKey] == "" {
		pod.Annotations[scheduling.KubeGroupNameAnnotationKey] = pgName
	} else {
		if pod.Annotations[scheduling.KubeGroupNameAnnotationKey] != pgName {
			klog.Errorf("normal pod %s/%s annotations %s value is not %s, but %s", pod.Namespace, pod.Name,
				scheduling.KubeGroupNameAnnotationKey, pgName, pod.Annotations[scheduling.KubeGroupNameAnnotationKey])
		}
		return nil
	}

	if _, err := pg.kubeClient.CoreV1().Pods(pod.Namespace).Update(context.TODO(), pod, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Failed to update pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		return err
	}

	return nil
}

func (pg *pgcontroller) createNormalPodPGIfNotExist(pod *v1.Pod) error {
	pgName := helpers.GeneratePodgroupName(pod)

	if _, err := pg.pgLister.PodGroups(pod.Namespace).Get(pgName); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}

		obj := &scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       pod.Namespace,
				Name:            pgName,
				OwnerReferences: newPGOwnerReferences(pod),
			},
			Spec: scheduling.PodGroupSpec{
				MinMember:         1,
				PriorityClassName: pod.Spec.PriorityClassName,
			},
		}
		if queueName, ok := pod.Annotations[scheduling.QueueNameAnnotationKey]; ok {
			obj.Spec.Queue = queueName
		}

		if _, err := pg.vcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Create(context.TODO(), obj, metav1.CreateOptions{}); err != nil {
			klog.Errorf("Failed to create normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}
	}

	return pg.updatePodAnnotations(pod, pgName)
}

func newPGOwnerReferences(pod *v1.Pod) []metav1.OwnerReference {
	if len(pod.OwnerReferences) != 0 {
		for _, ownerReference := range pod.OwnerReferences {
			if ownerReference.Controller != nil && *ownerReference.Controller {
				return pod.OwnerReferences
			}
		}
	}

	gvk := schema.GroupVersionKind{
		Group:   v1.SchemeGroupVersion.Group,
		Version: v1.SchemeGroupVersion.Version,
		Kind:    "Pod",
	}
	ref := metav1.NewControllerRef(pod, gvk)
	return []metav1.OwnerReference{*ref}
}
