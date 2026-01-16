/*
Copyright 2024 The Volcano Authors.

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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/scheduling"

	schedulerapi "volcano.sh/volcano/pkg/scheduler/api"
)

func TestReservationCacheAddAndDelete(t *testing.T) {
	rc := &ReservationCache{
		reservations: make(map[types.UID]*schedulerapi.ReservationInfo),
		nameToUID:    make(map[string]types.UID),
	}

	// Create a reservation info
	reservation := &scheduling.Reservation{}
	reservation.Name = "test-reservation"
	reservation.Namespace = "default"
	reservation.UID = types.UID("test-uid-123")

	reservationInfo := &schedulerapi.ReservationInfo{
		Reservation: reservation,
	}

	// Test Add
	rc.AddReservation(reservationInfo)

	// Verify reservation was added
	info, ok := rc.GetReservationById(types.UID("test-uid-123"))
	if !ok {
		t.Errorf("Expected to find reservation by ID")
	}
	if info.Reservation.Name != "test-reservation" {
		t.Errorf("Expected reservation name 'test-reservation', got '%s'", info.Reservation.Name)
	}

	// Test GetByName
	info2, ok := rc.GetReservationByName("test-reservation")
	if !ok {
		t.Errorf("Expected to find reservation by name")
	}
	if info2.Reservation.UID != types.UID("test-uid-123") {
		t.Errorf("Expected reservation UID 'test-uid-123', got '%s'", info2.Reservation.UID)
	}

	// Test Delete
	rc.DeleteReservation(types.UID("test-uid-123"))

	_, ok = rc.GetReservationById(types.UID("test-uid-123"))
	if ok {
		t.Errorf("Expected reservation to be deleted")
	}

	_, ok = rc.GetReservationByName("test-reservation")
	if ok {
		t.Errorf("Expected reservation to be deleted by name lookup")
	}
}

func TestReservationCacheGetByIdNotFound(t *testing.T) {
	rc := &ReservationCache{
		reservations: make(map[types.UID]*schedulerapi.ReservationInfo),
		nameToUID:    make(map[string]types.UID),
	}

	_, ok := rc.GetReservationById(types.UID("non-existent"))
	if ok {
		t.Errorf("Expected not to find non-existent reservation")
	}
}

func TestReservationCacheGetByNameNotFound(t *testing.T) {
	rc := &ReservationCache{
		reservations: make(map[types.UID]*schedulerapi.ReservationInfo),
		nameToUID:    make(map[string]types.UID),
	}

	_, ok := rc.GetReservationByName("non-existent")
	if ok {
		t.Errorf("Expected not to find non-existent reservation")
	}
}

func TestScanExpiredReservations(t *testing.T) {
	rc := &ReservationCache{
		reservations: make(map[types.UID]*schedulerapi.ReservationInfo),
		nameToUID:    make(map[string]types.UID),
	}

	now := time.Now()

	// Create a non-expired reservation
	reservation1 := &scheduling.Reservation{}
	reservation1.Name = "active-reservation"
	reservation1.Namespace = "default"
	reservation1.UID = types.UID("active-uid")
	reservation1.Status.State.Phase = scheduling.ReservationAvailable

	reservationInfo1 := &schedulerapi.ReservationInfo{
		Reservation: reservation1,
	}
	rc.AddReservation(reservationInfo1)

	// Test scan - neither should be expired
	var expiredCount int
	rc.ScanExpiredReservations(now, func(info *schedulerapi.ReservationInfo) {
		expiredCount++
	})

	// With default settings (no TTL set), nothing should be expired
	if expiredCount != 0 {
		t.Errorf("Expected 0 expired reservations, got %d", expiredCount)
	}
}

func TestScanExpiredReservationsWithCallback(t *testing.T) {
	rc := &ReservationCache{
		reservations: make(map[types.UID]*schedulerapi.ReservationInfo),
		nameToUID:    make(map[string]types.UID),
	}

	// Create a reservation
	reservation := &scheduling.Reservation{}
	reservation.Name = "test-reservation"
	reservation.Namespace = "default"
	reservation.UID = types.UID("test-uid")
	reservation.Status.State.Phase = scheduling.ReservationPending

	reservationInfo := &schedulerapi.ReservationInfo{
		Reservation: reservation,
	}
	rc.AddReservation(reservationInfo)

	// Verify we can iterate over reservations
	var foundReservations []*schedulerapi.ReservationInfo
	rc.ScanExpiredReservations(time.Now(), func(info *schedulerapi.ReservationInfo) {
		foundReservations = append(foundReservations, info)
	})

	// Since no TTL is set, none should be expired
	if len(foundReservations) != 0 {
		t.Errorf("Expected 0 found reservations (none expired), got %d", len(foundReservations))
	}
}
