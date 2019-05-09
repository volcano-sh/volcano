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

	vkbatchv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	kbv1 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha1"
)

type podRequest struct {
	pod *v1.Pod
}

func (cc *Controller) addPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		glog.Errorf("Failed to convert %v to v1.Pod", obj)
		return
	}

	req := podRequest{
		pod: pod,
	}

	cc.queue.Add(req)
}

func (cc *Controller) updatePodAnnotations(pod *v1.Pod, pgName string) error {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	if pod.Annotations[kbv1.GroupNameAnnotationKey] == "" {
		pod.Annotations[kbv1.GroupNameAnnotationKey] = pgName
	} else {
		return nil
	}

	if _, err := cc.kubeClients.CoreV1().Pods(pod.Namespace).Update(pod); err != nil {
		glog.Errorf("Failed to update pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		return err
	}

	return nil
}

func (cc *Controller) createNormalPodPGIfNotExist(pod *v1.Pod) error {
	pgName := vkbatchv1.PodgroupNamePrefix + string(pod.OwnerReferences[0].UID)

	if _, err := cc.pgLister.PodGroups(pod.Namespace).Get(pgName); err != nil {
		if !apierrors.IsNotFound(err) {
			glog.Errorf("Failed to get normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}

		pg := &kbv1.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       pod.Namespace,
				Name:            pgName,
				OwnerReferences: pod.OwnerReferences,
			},
			Spec: kbv1.PodGroupSpec{
				MinMember: 1,
			},
		}

		if _, err := cc.kbClients.SchedulingV1alpha1().PodGroups(pod.Namespace).Create(pg); err != nil {
			glog.Errorf("Failed to create normal PodGroup for Pod <%s/%s>: %v",
				pod.Namespace, pod.Name, err)
			return err
		}
	}

	return cc.updatePodAnnotations(pod, pgName)
}
