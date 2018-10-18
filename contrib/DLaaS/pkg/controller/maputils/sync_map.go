package maputils

import (
	"fmt"
	"sync"
)

//SyncCounterMap : map for keeping locks on structures
//
type SyncCounterMap struct {
	sync.Mutex
	m map[string]int
}

//NewSyncCounterMap : create a new sync map
//
func NewSyncCounterMap() *SyncCounterMap {
	return &SyncCounterMap{
		m: make(map[string]int),
	}
}

//Set : create a lock
//
func (sm *SyncCounterMap) Set(k string, v int) {
	sm.Mutex.Lock()
	defer sm.Mutex.Unlock()

	sm.m[k] = v
}

//Get : get a lock
//
func (sm *SyncCounterMap) Get(k string) (int, bool) {
	sm.Mutex.Lock()
	defer sm.Mutex.Unlock()

	v, ok := sm.m[k]
	return v, ok
}

func (sm *SyncCounterMap) delete(k string) {
	sm.Mutex.Lock()
	defer sm.Mutex.Unlock()
	delete(sm.m, k)
}

//DecreaseCounter :
//
func (sm *SyncCounterMap) DecreaseCounter(k string) (int, error) {
	sm.Mutex.Lock()
	defer sm.Mutex.Unlock()

	v, ok := sm.m[k]
	if !ok {
		return 0, fmt.Errorf("Fail to find counter for key %s", k)
	}

	if v > 0 {
		v--
	}

	if v == 0 {
		delete(sm.m, k)
	} else {
		sm.m[k] = v
	}

	return v, nil
}
