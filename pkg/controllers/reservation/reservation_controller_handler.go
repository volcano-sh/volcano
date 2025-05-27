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
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func (rc *reservationcontroller) addReservation(obj interface{}) {
	reservation, ok := obj.(*batch.Reservation)
	if !ok {
		klog.Errorf("obj is not Reservation")
		return
	}

	reservation = reservation.DeepCopy()

	// Find queue that reservation belongs to
	_, err := rc.GetQueueInfo(reservation.Spec.Queue)
	if err != nil {
		klog.Errorf(err.Error())
		return
	}

	// Skip reservation initiation if reservation is already initiated
	if !isInitiated(reservation) {
		if _, err := rc.initiateReservation(reservation); err != nil {
			klog.Errorf(err.Error())
			return
		}
	}

	return
}

func (rc *reservationcontroller) updateReservation(oldObj, newObj interface{}) {
	// TODO
	klog.V(3).Infof("Update Reservation, ignore. Not support now.")
	return
}

func (rc *reservationcontroller) deleteReservation(obj interface{}) {
	reservation, ok := obj.(*batch.Reservation)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %#v.", obj)
			return
		}
		reservation, ok = tombstone.Obj.(*batch.Reservation)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a Reservation: %#v.", obj)
			return
		}
	}

	if err := rc.deletePodGroup(reservation); err != nil {
		klog.Errorf(err.Error())
		return
	}
}
