/*
 Copyright 2021 The Volcano Authors.

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

package cache

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/helpers"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"

	schedulerapi "volcano.sh/volcano/pkg/scheduler/api"
)

type ReservationCache struct {
	sync.RWMutex
	vcClient        vcclientset.Interface
	reservations    map[types.UID]*schedulerapi.ReservationInfo
	nameToUID       map[string]types.UID
	nodesToTaskInfo map[string]*schedulerapi.TaskInfo
}

func newReservationCache(vcClient vcclientset.Interface) *ReservationCache {
	return &ReservationCache{
		RWMutex:         sync.RWMutex{},
		vcClient:        vcClient,
		reservations:    make(map[types.UID]*schedulerapi.ReservationInfo),
		nameToUID:       make(map[string]types.UID),
		nodesToTaskInfo: make(map[string]*schedulerapi.TaskInfo),
	}
}

func (rc *ReservationCache) AddReservation(reservation *schedulerapi.ReservationInfo) {
	rc.Lock()
	defer rc.Unlock()

	rc.reservations[reservation.Reservation.UID] = reservation
	rc.nameToUID[reservation.Reservation.Name] = reservation.Reservation.UID

}

func (rc *ReservationCache) DeleteReservation(reservationId types.UID) {
	rc.Lock()
	defer rc.Unlock()

	reservation, ok := rc.reservations[reservationId]
	if ok {
		delete(rc.reservations, reservation.Reservation.UID)
		delete(rc.nameToUID, reservation.Reservation.Name)
	}
}

func (rc *ReservationCache) GetReservationById(reservationId types.UID) (*schedulerapi.ReservationInfo, bool) {
	return rc.getReservationInfo(reservationId)
}

func (rc *ReservationCache) getReservationInfo(reservationId types.UID) (*schedulerapi.ReservationInfo, bool) {
	rc.RLock()
	defer rc.RUnlock()
	reservation, ok := rc.reservations[reservationId]
	if !ok {
		klog.Errorf("ReservationInfo with UID %s not found in cache", reservationId)
		return nil, false
	}
	return reservation, true
}

func (rc *ReservationCache) GetReservationByName(name string) (*schedulerapi.ReservationInfo, bool) {
	rc.RLock()
	defer rc.RUnlock()

	uid, ok := rc.nameToUID[name]
	if !ok {
		return nil, false
	}

	res, ok := rc.reservations[uid]
	return res, ok
}

func (rc *ReservationCache) SyncTaskStatus(task *schedulerapi.TaskInfo, job *schedulerapi.JobInfo) error {
	rc.Lock()
	defer rc.Unlock()

	reservation, ok := rc.getReservationByTask(task)
	if !ok {
		return fmt.Errorf("cannot find reservation from task <%s/%s>", task.Namespace, task.Name)
	}

	// re-calculate task status and update reservation status
	if err := rc.syncReservation(reservation, job); err != nil {
		klog.Errorf("Failed to update status of Reservation %v: %v",
			reservation.Reservation.UID, err)
		return err
	}
	return nil
}

func (rc *ReservationCache) syncReservation(reservation *schedulerapi.ReservationInfo, job *schedulerapi.JobInfo) error {
	rsve, err := rc.vcClient.BatchV1alpha1().Reservations(reservation.Reservation.Namespace).Get(context.TODO(), reservation.Reservation.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get Reservation %s/%s: %v", reservation.Reservation.Namespace, reservation.Reservation.Name, err)
		return err
	}
	oldStatus := reservation.Reservation.Status.DeepCopy()
	taskStatusCount := make(map[string]v1alpha1.TaskState)
	var pending, available, succeeded, failed int32
	calculateTasksStatus(job, &taskStatusCount, &pending, &available, &succeeded, &failed)
	newStatus := v1alpha1.ReservationStatus{
		State:           oldStatus.State,
		MinAvailable:    oldStatus.MinAvailable,
		TaskStatusCount: taskStatusCount,
		Pending:         pending,
		Available:       available,
		Succeeded:       succeeded,
		Failed:          failed,
		CurrentOwner:    oldStatus.CurrentOwner,
		Allocatable:     oldStatus.Allocatable,
		Allocated:       job.Allocated.ConvertResourceToResourceList(),
	}
	rsve.Status = newStatus
	rsve.Status.State = calculateReservationState(pending, available, succeeded, failed)
	reservationCondition := newCondition(rsve.Status.State.Phase, &rsve.Status.State.LastTransitionTime)
	rsve.Status.Conditions = append(rsve.Status.Conditions, reservationCondition)
	// 调用api更新
	newReservation, err := rc.vcClient.BatchV1alpha1().Reservations(rsve.Namespace).UpdateStatus(context.TODO(), rsve, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update status of Reservation %v/%v: %v",
			rsve.Namespace, rsve.Name, err)
		return err
	}
	// sync cache
	reservation.Reservation = newReservation
	return nil
}

func (rc *ReservationCache) AllocateJobToReservation(task *schedulerapi.TaskInfo, targetJob *schedulerapi.JobInfo) error {
	klog.V(5).Infof("AllocateJobToReservation: task <%s/%s> targetJob <%s/%s>", task.Namespace, task.Name, targetJob.Namespace, targetJob.Name)
	rc.Lock()
	defer rc.Unlock()
	reservation, ok := rc.getReservationByTask(task)
	if !ok {
		return fmt.Errorf("cannot find reservation from task <%s/%s>", task.Namespace, task.Name)
	}

	if reservation.Reservation.Status.CurrentOwner.Name == "" {
		pg := targetJob.PodGroup
		if len(pg.OwnerReferences) > 0 {
			ownerRef := pg.OwnerReferences[0]
			reservation.Reservation.Status.CurrentOwner = v1.ObjectReference{
				Kind:       ownerRef.Kind,
				Namespace:  pg.Namespace,
				Name:       ownerRef.Name,
				UID:        ownerRef.UID,
				APIVersion: ownerRef.APIVersion,
			}
		} else {
			reservation.Reservation.Status.CurrentOwner = v1.ObjectReference{
				Namespace: targetJob.Namespace,
				Name:      targetJob.Name,
				UID:       types.UID(targetJob.UID),
			}
		}
		klog.V(5).Infof("Setting current owner for reservation <%s/%s> to job <%s/%s>", reservation.Reservation.Namespace, reservation.Reservation.Name, targetJob.Namespace, targetJob.Name)
	}

	return nil
}

func (rc *ReservationCache) MatchReservationForPod(pod *v1.Pod) (*schedulerapi.ReservationInfo, bool) {

	rc.RLock()
	defer rc.RUnlock()
	for _, reservationInfo := range rc.reservations {
		if reservationInfo == nil || reservationInfo.Reservation == nil {
			continue
		}

		reservation := reservationInfo.Reservation
		for _, owner := range reservation.Spec.Owners {
			if owner.Object != nil &&
				owner.Object.Kind == "Pod" &&
				owner.Object.Name == pod.Name &&
				owner.Object.Namespace == pod.Namespace {
				return reservationInfo, true
			}
		}
	}

	return nil, false
}

// Assumes that lock is already acquired.
func (rc *ReservationCache) getReservationByTask(task *schedulerapi.TaskInfo) (*schedulerapi.ReservationInfo, bool) {
	if task == nil || task.Pod == nil {
		return nil, false
	}

	reservationUID := GetReservationUIDFromTask(task)
	if reservationUID == "" {
		return nil, false
	}
	reservation, ok := rc.reservations[reservationUID]
	return reservation, ok
}

func GetReservationUIDFromTask(taskInfo *schedulerapi.TaskInfo) types.UID {
	if taskInfo == nil || taskInfo.Pod == nil {
		return ""
	}

	for _, ownerRef := range taskInfo.Pod.OwnerReferences {
		if ownerRef.Kind == helpers.ReservationKind.Kind && ownerRef.APIVersion == helpers.ReservationKind.GroupVersion().String() {
			return ownerRef.UID
		}
	}

	return ""
}

func calculateTasksStatus(job *schedulerapi.JobInfo, taskStatusCount *map[string]v1alpha1.TaskState, pending *int32, available *int32, succeeded *int32, failed *int32) {
	for _, task := range job.Tasks {
		if task == nil || task.Pod == nil {
			continue
		}

		taskName, ok := task.Pod.Annotations[v1alpha1.TaskSpecKey]
		if !ok {
			continue
		}

		state, exists := (*taskStatusCount)[taskName]
		if !exists {
			state = v1alpha1.TaskState{Phase: make(map[v1.PodPhase]int32)}
		}

		switch task.Status {
		case schedulerapi.Pending:
			state.Phase[v1.PodPending]++
			*pending++
		case schedulerapi.Bound:
			state.Phase[v1.PodRunning]++
			*available++
		case schedulerapi.Succeeded:
			state.Phase[v1.PodSucceeded]++
			*succeeded++
		case schedulerapi.Failed:
			state.Phase[v1.PodFailed]++
			*failed++
		default:
			state.Phase[v1.PodUnknown]++
		}

		(*taskStatusCount)[taskName] = state
	}
}

func calculateReservationState(pending, available, succeeded, failed int32) v1alpha1.ReservationState {
	now := metav1.Now()
	total := pending + available + succeeded + failed

	var phase v1alpha1.ReservationPhase
	var reason, message string

	switch {
	case failed > 0:
		phase = v1alpha1.ReservationFailed
		reason = "TaskFailed"
		message = fmt.Sprintf("Reservation failed: %d/%d task(s) failed", failed, total)

	case succeeded == total && total > 0:
		phase = v1alpha1.ReservationSucceeded
		reason = "AllSucceeded"
		message = fmt.Sprintf("Reservation succeeded: all %d task(s) have be allocated", total)

	case available == total && total > 0:
		phase = v1alpha1.ReservationAvailable
		reason = "AllAvailable"
		message = fmt.Sprintf("Reservation available: all %d task(s) are available", available)

	case available > 0 && available < total:
		phase = v1alpha1.ReservationWaiting
		reason = "PartiallyAvailable"
		message = fmt.Sprintf("Reservation waiting: %d/%d task(s) are available", available, total)

	default:
		// available == 0
		phase = v1alpha1.ReservationPending
		reason = "NoAvailableTasks"
		message = fmt.Sprintf("Reservation pending: no tasks are available yet (total: %d)", total)
	}

	return v1alpha1.ReservationState{
		Phase:              phase,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}
}

func newCondition(status v1alpha1.ReservationPhase, lastTransitionTime *metav1.Time) v1alpha1.ReservationCondition {
	return v1alpha1.ReservationCondition{
		Status:             status,
		LastTransitionTime: lastTransitionTime,
	}
}
