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

package reservation

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/helpers"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/controllers/util"
)

// getPodGroupByReservation returns the podgroup related to the reservation.
// it will return normal pg if it exist in cluster,
// else it return legacy pg before version 1.5.
func (rc *reservationcontroller) getPodGroupByReservation(reservation *batch.Reservation) (*scheduling.PodGroup, error) {
	pgName := rc.generateRelatedPodGroupName(reservation)
	pg, err := rc.pgLister.PodGroups(reservation.Namespace).Get(pgName)
	if err == nil {
		return pg, nil
	}
	if apierrors.IsNotFound(err) {
		pg, err := rc.pgLister.PodGroups(reservation.Namespace).Get(reservation.Name)
		if err != nil {
			return nil, err
		}
		return pg, nil
	}
	return nil, err
}

func (rc *reservationcontroller) generateRelatedPodGroupName(reservation *batch.Reservation) string {
	return fmt.Sprintf("%s-%s", reservation.Name, string(reservation.UID))
}

func (rc *reservationcontroller) GetQueueInfo(queue string) (*scheduling.Queue, error) {
	queueInfo, err := rc.queueLister.Get(queue)
	if err != nil {
		klog.Errorf("Failed to get queue from queueLister, error: %s", err.Error())
	}

	return queueInfo, err
}

func isInitiated(rc *batch.Reservation) bool {
	if rc.Status.State.Phase == "" || rc.Status.State.Phase == batch.ReservationPending {
		return false
	}

	return true
}

func (rc *reservationcontroller) initiateReservation(reservation *batch.Reservation) (*batch.Reservation, error) {
	klog.V(3).Infof("Starting to initiate Reservation <%s/%s>", reservation.Namespace, reservation.Name)
	reservationInstance, err := rc.initReservationStatus(reservation)

	if err != nil {
		return nil, err
	}

	if err := rc.createOrUpdatePodGroup(reservationInstance); err != nil {
		return nil, err
	}

	return reservationInstance, nil
}

func (rc *reservationcontroller) initReservationStatus(reservation *batch.Reservation) (*batch.Reservation, error) {
	if reservation.Status.State.Phase != "" {
		return reservation, nil
	}
	reservation.Status.State.Phase = batch.ReservationPending
	reservation.Status.State.LastTransitionTime = metav1.Now()
	reservation.Status.MinAvailable = reservation.Spec.MinAvailable
	reservationCondition := newCondition(reservation.Status.State.Phase, &reservation.Status.State.LastTransitionTime)
	reservation.Status.Conditions = append(reservation.Status.Conditions, reservationCondition)

	// calculate the resources
	reservation.Status.Allocatable = calculateAllocatable(reservation)
	newReservation, err := rc.vcClient.BatchV1alpha1().Reservations(reservation.Namespace).UpdateStatus(context.TODO(), reservation, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update status of Reservation %v/%v: %v",
			reservation.Namespace, reservation.Name, err)
		return nil, err
	}

	return newReservation, nil
}

func newCondition(status batch.ReservationPhase, lastTransitionTime *metav1.Time) batch.ReservationCondition {
	return batch.ReservationCondition{
		Status:             status,
		LastTransitionTime: lastTransitionTime,
	}
}

func calculateAllocatable(reservation *batch.Reservation) v1.ResourceList {
	tasks := reservation.Spec.Tasks
	total := v1.ResourceList{}
	for _, task := range tasks {
		total = quotav1.Add(total, util.CalTaskRequests(&v1.Pod{Spec: task.Template.Spec}, task.Replicas))
	}
	return total
}

func (rc *reservationcontroller) createOrUpdatePodGroup(reservation *batch.Reservation) error {
	// If PodGroup does not exist, create one for Reservation.
	pg, err := rc.getPodGroupByReservation(reservation)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get PodGroup for Reservation <%s/%s>: %v",
				reservation.Namespace, reservation.Name, err)
			return err
		}
		minTaskMember := map[string]int32{}
		for _, task := range reservation.Spec.Tasks {
			minTaskMember[task.Name] = task.Replicas
		}
		minReq := calculateAllocatable(reservation)

		annotations := make(map[string]string)
		for k, v := range reservation.Annotations {
			annotations[k] = v
		}
		annotations[scheduling.VolcanoGroupReservationOnlyAnnotationKey] = "true"

		pg := &scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: reservation.Namespace,
				// add reservation.UID into its name when create new PodGroup
				Name:        rc.generateRelatedPodGroupName(reservation),
				Annotations: annotations,
				Labels:      reservation.Labels,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(reservation, helpers.ReservationKind),
				},
			},
			Spec: scheduling.PodGroupSpec{
				MinMember:     reservation.Spec.MinAvailable,
				MinTaskMember: minTaskMember,
				Queue:         reservation.Spec.Queue,
				MinResources:  &minReq,
			},
		}

		if _, err = rc.vcClient.SchedulingV1beta1().PodGroups(reservation.Namespace).Create(context.TODO(), pg, metav1.CreateOptions{}); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				klog.Errorf("Failed to create PodGroup for Reservation <%s/%s>: %v",
					reservation.Namespace, reservation.Name, err)
				return err
			}
		}
		return nil
	}

	pgShouldUpdate := false

	minResources := calculateAllocatable(reservation)
	if pg.Spec.MinMember != reservation.Spec.MinAvailable || !equality.Semantic.DeepEqual(pg.Spec.MinResources, minResources) {
		pg.Spec.MinMember = reservation.Spec.MinAvailable
		pg.Spec.MinResources = &minResources
		pgShouldUpdate = true
	}

	if pg.Spec.MinTaskMember == nil {
		pgShouldUpdate = true
		pg.Spec.MinTaskMember = make(map[string]int32)
	}

	for _, task := range reservation.Spec.Tasks {
		cnt := task.Replicas

		if taskMember, ok := pg.Spec.MinTaskMember[task.Name]; !ok {
			pgShouldUpdate = true
			pg.Spec.MinTaskMember[task.Name] = cnt
		} else {
			if taskMember == cnt {
				continue
			}

			pgShouldUpdate = true
			pg.Spec.MinTaskMember[task.Name] = cnt
		}
	}

	if !pgShouldUpdate {
		return nil
	}

	_, err = rc.vcClient.SchedulingV1beta1().PodGroups(reservation.Namespace).Update(context.TODO(), pg, metav1.UpdateOptions{})
	if err != nil {
		klog.V(3).Infof("Failed to update PodGroup for Reservation <%s/%s>: %v",
			reservation.Namespace, reservation.Name, err)
	}
	return err
}

func (rc *reservationcontroller) deletePodGroup(reservation *batch.Reservation) error {
	pgName := rc.generateRelatedPodGroupName(reservation)
	if err := rc.vcClient.SchedulingV1beta1().PodGroups(reservation.Namespace).Delete(context.TODO(), pgName, metav1.DeleteOptions{}); err != nil {
		klog.Errorf("delete podgroup <%s/%s> from reservation <%s/%s> failed: %v", reservation.Namespace, pgName, reservation.Namespace, reservation.Name, err)
		return err
	}
	return nil
}
