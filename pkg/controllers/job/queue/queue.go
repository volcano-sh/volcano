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

package queue

import (
	"sync"
	"time"

	"github.com/golang/glog"

	"k8s.io/client-go/util/workqueue"
)

type queue struct {
	sync.Mutex

	index   workqueue.RateLimitingInterface
	data    map[interface{}]workqueue.Interface
	indexFn func(interface{}) interface{}
}

func New(indexFn func(interface{}) interface{}) workqueue.RateLimitingInterface {
	return &queue{
		index:   workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		data:    make(map[interface{}]workqueue.Interface),
		indexFn: indexFn,
	}
}

func (q *queue) getDataQueue(key interface{}) workqueue.Interface {
	q.Lock()
	defer q.Unlock()

	if _, found := q.data[key]; !found {
		q.data[key] = workqueue.New()
	}

	return q.data[key]
}

// AddRateLimited adds an item to the workqueue after the rate limiter says its ok
func (q *queue) AddRateLimited(item interface{}) {
	glog.V(4).Infof("queue.AddRateLimited entering ... ")
	defer glog.V(4).Infof("queue.AddRateLimited finished")

	key := q.indexFn(item)

	q.index.AddRateLimited(key)
	q.getDataQueue(key).Add(item)
}

// Forget indicates that an item is finished being retried. Doesn't matter whether its for perm failing
// or for success, we'll stop the rate limiter from tracking it.  This only clears the `rateLimiter`, you
// still have to call `Done` on the queue.
func (q *queue) Forget(item interface{}) {
	glog.V(4).Infof("queue.Forget entering ... ")
	defer glog.V(4).Infof("queue.Forget finished")

	key := q.indexFn(item)
	q.index.Forget(key)
}

// NumRequeues returns back how many times the item was requeued
func (q *queue) NumRequeues(item interface{}) int {
	glog.V(4).Infof("queue.NumRequeues entering ... ")
	defer glog.V(4).Infof("queue.NumRequeues finished")

	return q.index.NumRequeues(q.indexFn(item))
}

// AddAfter adds an item to the workqueue after the indicated duration has passed
func (q *queue) AddAfter(item interface{}, duration time.Duration) {
	glog.V(4).Infof("queue.AddAfter entering ... ")
	defer glog.V(4).Infof("queue.AddAfter finished")

	key := q.indexFn(item)
	q.index.AddAfter(key, duration)
	q.getDataQueue(key).Add(item)
}

func (q *queue) Add(item interface{}) {
	glog.V(4).Infof("queue.Add <%v> entering ... ", item)
	defer glog.V(4).Infof("queue.Add <%v> finished", item)

	key := q.indexFn(item)
	q.index.Add(key)
	q.getDataQueue(key).Add(item)
}

func (q *queue) Len() int {
	glog.V(4).Infof("queue.Len entering ... ")
	defer glog.V(4).Infof("queue.Len finished")

	q.Lock()
	defer q.Unlock()

	sum := 0

	for _, d := range q.data {
		sum += d.Len()
	}

	return sum
}

func (q *queue) Get() (item interface{}, shutdown bool) {
	glog.V(4).Infof("queue.Get entering ... ")
	defer glog.V(4).Infof("queue.Get finished")

	key, sd := q.index.Get()
	if sd {
		return key, sd
	}

	glog.V(4).Infof("try to get item by key <%v>", key)

	item, shutdown = q.getDataQueue(key).Get()

	glog.V(4).Infof("get item <%v> by key <%v>", item, key)

	return
}

func (q *queue) Done(item interface{}) {
	glog.V(4).Infof("queue.Done entering ... ")
	defer glog.V(4).Infof("queue.Done finished")

	key := q.indexFn(item)
	q.getDataQueue(key).Done(item)
	q.index.Done(key)

	glog.V(4).Infof("item <%v> is done in queue <%v>", item, key)

	q.Lock()
	defer q.Unlock()
	// If still data, add it back.
	if q.data[key].Len() != 0 {
		q.index.Add(key)
	}
}

func (q *queue) ShutDown() {
	glog.V(4).Infof("queue.ShutDown entering ... ")
	defer glog.V(4).Infof("queue.ShutDown finished")

	q.index.ShutDown()
}

func (q *queue) ShuttingDown() bool {
	glog.V(4).Infof("queue.ShuttingDown entering ... ")
	defer glog.V(4).Infof("queue.ShuttingDown finished")

	return q.index.ShuttingDown()
}
