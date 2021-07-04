/*
Copyright 2018 The Kubernetes Authors.

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

package api

import (
	"fmt"
	"reflect"
	"sync"
)

// OrderMap has the function of a map and key is sorted
type OrderMap struct {
	keys   []string
	values map[string]interface{}
	sync.RWMutex
}

// NewOrderMap return OrderMap
func NewOrderMap() *OrderMap {
	return &OrderMap{
		keys:   make([]string, 0),
		values: make(map[string]interface{}),
	}
}

// String return string
func (om *OrderMap) String() string {
	om.RLock()
	defer om.RUnlock()
	return fmt.Sprintf("%v", om.keys)
}

// AddIfNotPresent add item to OrderMap when not exist
func (om *OrderMap) AddIfNotPresent(key string, item interface{}) {
	om.Lock()
	defer om.Unlock()
	if _, ok := om.values[key]; !ok {
		om.values[key] = item
		om.keys = append(om.keys, key)
	}
}

// Delete delete item from OrderMap
func (om *OrderMap) Delete(key string) bool {
	om.Lock()
	defer om.Unlock()
	if _, ok := om.values[key]; ok {
		for i, v := range om.keys {
			if v == key {
				om.keys = append(om.keys[0:i], om.keys[i+1:]...)
				break
			}
		}
		delete(om.values, key)
		return true
	}
	return false
}

// Get return object by key
func (om *OrderMap) Get(key string) interface{} {
	om.RLock()
	defer om.RUnlock()
	if _, ok := om.values[key]; ok {
		return om.values[key]
	}
	return nil
}

// List return list of keys
func (om *OrderMap) List() []string {
	om.RLock()
	defer om.RUnlock()
	return om.keys
}

// Exist return true if key exist
func (om *OrderMap) Exist(key string) bool {
	om.RLock()
	defer om.RUnlock()
	if _, ok := om.values[key]; ok {
		return true
	}
	return false
}

// Index return the index of key, from 0 to n-1;
// return -1 if key not exist
func (om *OrderMap) Index(key string) int {
	om.RLock()
	defer om.RUnlock()
	if _, ok := om.values[key]; ok {
		for i, v := range om.keys {
			if v == key {
				return i
			}
		}
	}
	return -1
}

// Update update key with item
func (om *OrderMap) Update(key string, item interface{}) {
	om.Lock()
	defer om.Unlock()
	if _, ok := om.values[key]; ok {
		om.values[key] = item
	} else {
		om.values[key] = item
		om.keys = append(om.keys, key)
	}
}

// Len return lenth
func (om *OrderMap) Len() int {
	om.RLock()
	defer om.RUnlock()
	return len(om.keys)
}

// Clone clone OrderMap
func (om *OrderMap) Clone() *OrderMap {
	om.RLock()
	defer om.RUnlock()
	length := len(om.keys)
	out := &OrderMap{
		keys:   make([]string, length),
		values: make(map[string]interface{}, length),
	}
	copy(out.keys, om.keys)
	for _, key := range om.keys {
		out.values[key] = om.values[key]
	}

	return out
}

// Equal check the keys same as other or not
func (om *OrderMap) Equal(other *OrderMap) bool {
	om.RLock()
	defer om.RUnlock()
	if len(om.keys) != len(other.keys) {
		return false
	}
	for i, key := range om.keys {
		if key != other.keys[i] {
			return false
		}
	}
	return true
}

// DeepEqual check keys and values same as ohter or not
func (om *OrderMap) DeepEqual(other *OrderMap) bool {
	om.RLock()
	defer om.RUnlock()
	if len(om.keys) != len(other.keys) {
		return false
	}
	for i, key := range om.keys {
		if key != other.keys[i] {
			return false
		}
		if !reflect.DeepEqual(om.values[key], other.values[key]) {
			return false
		}
	}
	return true
}
