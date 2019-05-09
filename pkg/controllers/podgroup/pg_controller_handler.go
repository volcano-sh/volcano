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
	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/helpers"
	scheduling "volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
)

type podRequest struct {
	podName      string
	podNamespace string
}

func (cc *Controller) addPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		glog.Errorf("Failed to convert %v to v1.Pod", obj)
		return
	}

	req := podRequest{
		podName:      pod.Name,
		podNamespace: pod.Namespace,
	}

	cc.queue.Add(req)
}

func (cc *Controller) updatePodAnnotations(pod *v1.Pod, pgName string) error {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	if pod.Annotations[scheduling.GroupNameAnnotationKey] == "" {
		pod.Annotations[scheduling.GroupNameAnnotationKey] = pgName
	} else {
		if pod.Annotations[scheduling.GroupNameAnnotationKey] != pgName {
			glog.Errorf("normal pod %s/%s annotations %s value is not %s, but %s", pod.Namespace, pod.Name,
				scheduling.GroupNameAnnotationKey, pgName, pod.Annotations[scheduling.GroupNameAnnotationKey])
		}
		return nil
	}

	if _, err := cc.kubeClients.CoreV1().Pods(pod.Namespace).Update(pod); err != nil {
		glog.Errorf("Failed to update pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		return err
	}

	return nil
}

func (cc *Controller) createNormalPodPGIfNotExist(pod *v1.Pod) error {
	pgName := helpers.GeneratePodgroupName(pod)

	if _, err := cc.pgLister.PodGroups(pod.Namespace).Get(pgName); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.Errorf("Failed to get normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}

		pg := &scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       pod.Namespace,
				Name:            pgName,
				OwnerReferences: newPGOwnerReferences(pod),
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 1,
			},
		}

		if _, err := cc.kbClients.SchedulingV1alpha2().PodGroups(pod.Namespace).Create(pg); err != nil {
			glog.Errorf("Failed to create normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}
	}

	return cc.updatePodAnnotations(pod, pgName)
}

func newPGOwnerReferences(pod *v1.Pod) []metav1.OwnerReference {
	if len(pod.OwnerReferences) != 0 {
		for _, ownerReference := range pod.OwnerReferences {
			if ownerReference.Controller != nil && *ownerReference.Controller == true {
				return pod.OwnerReferences
			}
		}
	}

	isController := true
	return []metav1.OwnerReference{{
		APIVersion: helpers.JobKind.GroupVersion().String(),
		Controller: &isController,
		UID:        pod.UID,
	}}
}
