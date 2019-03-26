/*
Copyright 2017 The Volcano Authors.

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

package cronjob

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"sync"
	"time"
	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"

	kbver "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	vkver "volcano.sh/volcano/pkg/client/clientset/versioned"
	vkscheme "volcano.sh/volcano/pkg/client/clientset/versioned/scheme"
	vkinfoext "volcano.sh/volcano/pkg/client/informers/externalversions"
	vkbatchinfo "volcano.sh/volcano/pkg/client/informers/externalversions/batch/v1alpha1"
	vkbatchlister "volcano.sh/volcano/pkg/client/listers/batch/v1alpha1"
)

const (
	ProcessInterval = time.Second * 2
)

// Controller for cron job
type Controller struct {
	config      *rest.Config
	kubeClients *kubernetes.Clientset
	vkClients   *vkver.Clientset
	kbClients   *kbver.Clientset

	cronJobInformer vkbatchinfo.CronJobInformer

	// A store of cron jobs
	cronJobLister vkbatchlister.CronJobLister
	cronJobSynced func() bool

	// queue that need to sync up
	queue        workqueue.RateLimitingInterface
	//CronJob Event recorder
	recorder record.EventRecorder

	// CronJob Store
	jobStore cronJobStore

	clock    clock.Clock
}

// NewCronJobController create new CronJob Controller
func NewCronJobController(config *rest.Config) *Controller {

	kubeClients := kubernetes.NewForConfigOrDie(config)

	//Initialize event client
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: kubeClients.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(vkscheme.Scheme, v1.EventSource{Component: "vk-controller"})

	cc := &Controller{
		config:       config,
		kubeClients:  kubeClients,
		vkClients:    vkver.NewForConfigOrDie(config),
		kbClients:    kbver.NewForConfigOrDie(config),
		queue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder:     recorder,
		jobStore:     cronJobStore{
			cJobs: map[string]*v1alpha1.CronJob{},
		},
		clock: clock.RealClock{},
	}

	cc.cronJobInformer = vkinfoext.NewSharedInformerFactory(cc.vkClients, 0).Batch().V1alpha1().CronJobs()
	cc.cronJobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addCronJob,
		UpdateFunc: cc.updateCronJob,
		DeleteFunc: cc.deleteCronJob,
	})
	cc.cronJobLister = cc.cronJobInformer.Lister()
	cc.cronJobSynced = cc.cronJobInformer.Informer().HasSynced
	return cc
}

// Run start Controller
func (cc *Controller) Run(stopCh <-chan struct{}) {
	go cc.cronJobInformer.Informer().Run(stopCh)

	cache.WaitForCacheSync(stopCh, cc.cronJobSynced)

	go wait.Until(cc.handleCronJobs, ProcessInterval, stopCh)

	glog.Infof("CronJobController is running ...... ")
}

func (cc *Controller) handleCronJobs() {
	for cc.handleSingleCronJob() {
	}
}

func (cc *Controller) handleSingleCronJob() bool {
	obj, shutdown := cc.queue.Get()
	if shutdown {
		glog.Errorf("Fail to CronJob key from queue")
		return false
	}
	defer cc.queue.Done(obj)
	glog.V(3).Infof("Try to handle CronJob: <%v>", obj)

	err := cc.syncCronJob(obj.(string))
	if err != nil {
		cc.queue.AddRateLimited(obj)
		return true
	}
	// If no error, forget it.
	cc.queue.Forget(obj)
	return true
}


type cronJobStore struct {
	sync.Mutex
	cJobs        map[string]*v1alpha1.CronJob
}

func (cs *cronJobStore) AddOrUpdate(key string, job *v1alpha1.CronJob){
	cs.Lock()
	defer cs.Unlock()
	cs.cJobs[key] = job
}

func (cs *cronJobStore) Get(key string) (*v1alpha1.CronJob, error) {
	cs.Lock()
	defer cs.Unlock()
	job, found := cs.cJobs[key]
	if !found {
		return nil, fmt.Errorf("could not found CronJob %s in cache", key)
	}
	return job, nil
}

func (cs *cronJobStore) Delete(key string) {
	cs.Lock()
	defer cs.Unlock()
	delete(cs.cJobs, key)
}
