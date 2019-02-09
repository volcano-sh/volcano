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
	router  map[interface{}]workqueue.Interface
	indexFn func(interface{}) interface{}
}

func New(indexFn func(interface{}) interface{}) workqueue.RateLimitingInterface {
	return &queue{
		index:   workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		router:  make(map[interface{}]workqueue.Interface),
		indexFn: indexFn,
	}
}

func (q *queue) getQueue(key interface{}) workqueue.Interface {
	q.Lock()
	defer q.Unlock()

	if _, found := q.router[key]; !found {
		q.router[key] = workqueue.New()
	}

	return q.router[key]
}

// AddRateLimited adds an item to the workqueue after the rate limiter says its ok
func (q *queue) AddRateLimited(item interface{}) {
	glog.V(3).Infof("queue.AddRateLimited entering ... ")
	defer glog.V(3).Infof("queue.AddRateLimited finished")

	key := q.indexFn(item)

	q.index.AddRateLimited(key)
	q.getQueue(key).Add(item)
}

// Forget indicates that an item is finished being retried. Doesn't matter whether its for perm failing
// or for success, we'll stop the rate limiter from tracking it.  This only clears the `rateLimiter`, you
// still have to call `Done` on the queue.
func (q *queue) Forget(item interface{}) {
	glog.V(3).Infof("queue.Forget entering ... ")
	defer glog.V(3).Infof("queue.Forget finished")

	key := q.indexFn(item)
	q.index.Forget(key)
}

// NumRequeues returns back how many times the item was requeued
func (q *queue) NumRequeues(item interface{}) int {
	glog.V(3).Infof("queue.NumRequeues entering ... ")
	defer glog.V(3).Infof("queue.NumRequeues finished")

	return q.index.NumRequeues(q.indexFn(item))
}

// AddAfter adds an item to the workqueue after the indicated duration has passed
func (q *queue) AddAfter(item interface{}, duration time.Duration) {
	glog.V(3).Infof("queue.AddAfter entering ... ")
	defer glog.V(3).Infof("queue.AddAfter finished")

	key := q.indexFn(item)
	q.index.AddAfter(key, duration)
	q.getQueue(key).Add(item)
}

func (q *queue) Add(item interface{}) {
	glog.V(3).Infof("queue.Add <%v> entering ... ", item)
	defer glog.V(3).Infof("queue.Add <%v> finished", item)

	key := q.indexFn(item)
	q.index.Add(key)
	q.getQueue(key).Add(item)
}

func (q *queue) Len() int {
	glog.V(3).Infof("queue.Len entering ... ")
	defer glog.V(3).Infof("queue.Len finished")

	q.Lock()
	defer q.Unlock()

	sum := 0

	for _, d := range q.router {
		sum += d.Len()
	}

	return sum
}

func (q *queue) Get() (item interface{}, shutdown bool) {
	glog.V(3).Infof("queue.Get entering ... ")
	defer glog.V(3).Infof("queue.Get finished")

	key, sd := q.index.Get()
	if sd {
		return key, sd
	}

	glog.V(3).Infof("try to get item by key <%v>", key)

	item, shutdown = q.getQueue(key).Get()

	glog.V(3).Infof("get item <%v> by key <%v>", item, key)

	return
}

func (q *queue) Done(item interface{}) {
	glog.V(3).Infof("queue.Done entering ... ")
	defer glog.V(3).Infof("queue.Done finished")

	key := q.indexFn(item)
	q.getQueue(key).Done(item)
	q.index.Done(key)

	glog.V(3).Infof("item <%v> is done in queue <%v>", item, key)

	q.Lock()
	defer q.Unlock()
	// If still router, add it back.
	if q.router[key].Len() != 0 {
		q.index.Add(key)
	}
}

func (q *queue) ShutDown() {
	glog.V(3).Infof("queue.ShutDown entering ... ")
	defer glog.V(3).Infof("queue.ShutDown finished")

	q.index.ShutDown()
}

func (q *queue) ShuttingDown() bool {
	glog.V(3).Infof("queue.ShuttingDown entering ... ")
	defer glog.V(3).Infof("queue.ShuttingDown finished")

	return q.index.ShuttingDown()
}
