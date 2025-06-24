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
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	"volcano.sh/apis/pkg/apis/scheduling"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"

	schedulerapi "volcano.sh/volcano/pkg/scheduler/api"
)

type ReservationCache struct {
	sync.RWMutex
	vcClient     vcclientset.Interface
	reservations map[types.UID]*schedulerapi.ReservationInfo
	nameToUID    map[string]types.UID
}

func newReservationCache(vcClient vcclientset.Interface) *ReservationCache {
	return &ReservationCache{
		RWMutex:      sync.RWMutex{},
		vcClient:     vcClient,
		reservations: make(map[types.UID]*schedulerapi.ReservationInfo),
		nameToUID:    make(map[string]types.UID),
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

// todo: Async use Queue
func (rc *ReservationCache) syncReservation(reservation *schedulerapi.ReservationInfo, job *schedulerapi.JobInfo) error {
	rsveV1beta1, err := rc.vcClient.SchedulingV1beta1().Reservations(reservation.Reservation.Namespace).Get(context.TODO(), reservation.Reservation.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get Reservation %s/%s: %v", reservation.Reservation.Namespace, reservation.Reservation.Name, err)
		return err
	}

	rsve, err := ConvertToInternalReservation(rsveV1beta1)
	if err != nil {
		klog.Errorf("Failed to convert reservation from %T to %T", rsveV1beta1, rsve)
		return err
	}

	oldStatus := reservation.Reservation.Status.DeepCopy()
	taskStatusCount := make(map[string]v1alpha1.TaskState)
	var pending, available, succeeded, failed int32
	calculateTasksStatus(job, &taskStatusCount, &pending, &available, &succeeded, &failed)
	newStatus := scheduling.ReservationStatus{
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

	newReservationV1, err := ConvertToV1beta1Reservation(rsve)
	if err != nil {
		klog.Errorf("Failed to convert reservation from %T to %T", rsve, newReservationV1)
		return err
	}
	// update reservation status in API server
	newReservationV1beta1, err := rc.vcClient.SchedulingV1beta1().Reservations(rsve.Namespace).UpdateStatus(context.TODO(), newReservationV1, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update status of Reservation %v/%v: %v",
			rsve.Namespace, rsve.Name, err)
		return err
	}

	newReservation, err := ConvertToInternalReservation(newReservationV1beta1)
	if err != nil {
		klog.Errorf("Failed to convert reservation from %T to %T", newReservationV1beta1, newReservation)
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

func (rc *ReservationCache) ScanExpiredReservations(now time.Time, onExpired func(*schedulerapi.ReservationInfo)) {
	rc.RLock()
	defer rc.RUnlock()

	for _, reservation := range rc.reservations {
		if isReservationNeedExpiration(reservation, now) {
			onExpired(reservation)
		}
	}
}

func (rc *ReservationCache) GcExpiredReservation(reservation *schedulerapi.ReservationInfo) error {
	rc.Lock()
	defer rc.Unlock()

	// Remove reservation from cache
	delete(rc.reservations, reservation.Reservation.UID)
	delete(rc.nameToUID, reservation.Reservation.Name)

	// Sync status to API server
	rsveV1beta1, err := rc.vcClient.SchedulingV1beta1().Reservations(reservation.Reservation.Namespace).Get(context.TODO(), reservation.Reservation.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(3).Infof("Reservation %s/%s not found in API server, maybe it's already deleted.",
				reservation.Reservation.Namespace, reservation.Reservation.Name)
			return nil
		}
		klog.Errorf("Failed to get Reservation %s/%s for GC: %v",
			reservation.Reservation.Namespace, reservation.Reservation.Name, err)
		return err
	}

	rsve, err := ConvertToInternalReservation(rsveV1beta1)
	if err != nil {
		klog.Errorf("Failed to convert reservation from %T to %T", rsveV1beta1, rsve)
		return err
	}

	now := metav1.Now()
	rsve.Status.State.Phase = scheduling.ReservationFailed
	rsve.Status.State.Reason = "Expired"
	rsve.Status.State.Message = "Reservation expired and was cleaned up by the scheduler"
	rsve.Status.State.LastTransitionTime = now
	reservationCondition := newCondition(rsve.Status.State.Phase, &rsve.Status.State.LastTransitionTime)
	rsve.Status.Conditions = append(rsve.Status.Conditions, reservationCondition)

	newReservationV1, err := ConvertToV1beta1Reservation(rsve)
	if err != nil {
		klog.Errorf("Failed to convert reservation from %T to %T", rsve, newReservationV1)
		return err
	}
	_, err = rc.vcClient.SchedulingV1beta1().Reservations(rsve.Namespace).UpdateStatus(context.TODO(), newReservationV1, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update status of Reservation %v/%v: %v",
			rsve.Namespace, rsve.Name, err)
		return err
	}
	return nil
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

func calculateReservationState(pending, available, succeeded, failed int32) scheduling.ReservationState {
	now := metav1.Now()
	total := pending + available + succeeded + failed

	var phase scheduling.ReservationPhase
	var reason, message string

	switch {
	case failed > 0:
		phase = scheduling.ReservationFailed
		reason = "TaskFailed"
		message = fmt.Sprintf("Reservation failed: %d/%d task(s) failed", failed, total)

	case succeeded == total && total > 0:
		phase = scheduling.ReservationSucceeded
		reason = "AllSucceeded"
		message = fmt.Sprintf("Reservation succeeded: all %d task(s) have be allocated", total)

	case available == total && total > 0:
		phase = scheduling.ReservationAvailable
		reason = "AllAvailable"
		message = fmt.Sprintf("Reservation available: all %d task(s) are available", available)

	case available > 0 && available < total:
		phase = scheduling.ReservationWaiting
		reason = "PartiallyAvailable"
		message = fmt.Sprintf("Reservation waiting: %d/%d task(s) are available", available, total)

	default:
		// available == 0
		phase = scheduling.ReservationPending
		reason = "NoAvailableTasks"
		message = fmt.Sprintf("Reservation pending: no tasks are available yet (total: %d)", total)
	}

	return scheduling.ReservationState{
		Phase:              phase,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}
}

func newCondition(status scheduling.ReservationPhase, lastTransitionTime *metav1.Time) scheduling.ReservationCondition {
	return scheduling.ReservationCondition{
		Status:             status,
		LastTransitionTime: lastTransitionTime,
	}
}

func isReservationNeedExpiration(reservation *schedulerapi.ReservationInfo, now time.Time) bool {
	// 1. Skip failed or succeeded reservations
	rs := reservation.Reservation
	if rs.Status.State.Phase == scheduling.ReservationFailed || rs.Status.State.Phase == scheduling.ReservationSucceeded {
		return false
	}

	// 2. Skip if TTL is set to 0
	if rs.Spec.TTL != nil && rs.Spec.TTL.Duration == 0 {
		return false
	}

	// 3. Check expiration via Expires field (preferred)
	if rs.Spec.Expires != nil {
		expireAt := rs.Spec.Expires.Time.UTC()
		if now.UTC().After(expireAt) {
			return true
		}
	}

	// 4. Fallback to TTL-based expiration
	if rs.Spec.TTL != nil {
		createAt := rs.CreationTimestamp.Time.UTC()
		ttlExpireAt := createAt.Add(rs.Spec.TTL.Duration)
		if now.UTC().After(ttlExpireAt) {
			return true
		}
	}

	return false
}
