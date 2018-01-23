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

package util

import (
	"strings"
)

// An Item is something we manage in a fifo queue.
type DictionaryItem struct {
	Value interface{} // The value of the item
	Name  string      // The name of the item in the queue.
}

func NewDictionaryItem(v interface{}, n string) *DictionaryItem {
	return &DictionaryItem{
		Value: v,
		Name:  n,
	}
}

type DictionaryQueue []*DictionaryItem

func NewDictionaryQueue() DictionaryQueue {
	return make(DictionaryQueue, 0)
}

func (dq DictionaryQueue) Len() int { return len(dq) }

func (dq DictionaryQueue) Less(i, j int) bool {
	return strings.Compare(dq[i].Name, dq[j].Name) < 0
}

func (dq DictionaryQueue) Swap(i, j int) {
	dq[i], dq[j] = dq[j], dq[i]
}

func (dq *DictionaryQueue) Push(x interface{}) {
	item := x.(*DictionaryItem)
	*dq = append(*dq, item)
}
