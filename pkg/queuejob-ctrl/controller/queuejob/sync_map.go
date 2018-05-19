/*
Copyright 2017 The Kubernetes Authors.

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

package queuejob

import (
	"fmt"
	"sync"
)

type syncCounterMap struct {
	sync.Mutex
	m map[string]int
}

func newSyncCounterMap() *syncCounterMap {
	return &syncCounterMap{
		m: make(map[string]int),
	}
}

func (sm *syncCounterMap) set(k string, v int) {
	sm.Mutex.Lock()
	defer sm.Mutex.Unlock()

	sm.m[k] = v
}

func (sm *syncCounterMap) get(k string) (int, bool) {
	sm.Mutex.Lock()
	defer sm.Mutex.Unlock()

	v, ok := sm.m[k]
	return v, ok
}

func (sm *syncCounterMap) delete(k string) {
	sm.Mutex.Lock()
	defer sm.Mutex.Unlock()

	delete(sm.m, k)
}

func (sm *syncCounterMap) decreaseCounter(k string) (int, error) {
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
