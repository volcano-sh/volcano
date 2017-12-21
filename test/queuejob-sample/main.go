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

package main

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	qInformerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers"
	"github.com/spf13/pflag"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"strconv"
)

type jobStatus string

const (
	JobStatusPending jobStatus = "Pending"
	JobStatusRunning jobStatus = "Running"
	JobStatusDone    jobStatus = "Done"
)

type QueueJobInfo struct {
	priority     int
	queueJobName string
	jobName      string
	taskNo       int32
	status       jobStatus
}

type BatchJobSample struct {
	namespace     string
	kubeconfig    string
	queue         string
	taskno        int32
	sleeptime     string
	config        *rest.Config
	queueJobToJob []*QueueJobInfo
}

func (t *BatchJobSample) printQueueJobStatus() {
	for _, ts := range t.queueJobToJob {
		fmt.Printf("----> *** The job <%s> under queuejob <%s> is <%s>, priority=<%d>\n", ts.jobName, ts.queueJobName, ts.status, ts.priority)
	}
	fmt.Println()
}

func (t *BatchJobSample) doCreateJob(queueJobInfo *QueueJobInfo) error {
	// create job test
	startCmd := "echo sleep_start ; sleep " + t.sleeptime + " ; echo sleep_done"
	container := corev1.Container{
		Name:  "sleepdeamon",
		Image: "centos:7",
		Resources: corev1.ResourceRequirements{
			Limits: map[corev1.ResourceName]resource.Quantity{
				"cpu":    resource.MustParse("1"),
				"memory": resource.MustParse("1Gi"),
			},
			Requests: map[corev1.ResourceName]resource.Quantity{
				"cpu":    resource.MustParse("1"),
				"memory": resource.MustParse("1Gi"),
			},
		},
		Command: []string{
			"bin/bash",
			"-c",
			startCmd,
		},
	}
	testJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      queueJobInfo.jobName,
			Namespace: t.namespace,
		},
		Spec: batchv1.JobSpec{
			Completions: &t.taskno,
			Parallelism: &t.taskno,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "sleep",
					Labels: map[string]string{
						"preemptionrank": strconv.Itoa(queueJobInfo.priority),
					},
				},
				Spec: corev1.PodSpec{
					Containers:    []corev1.Container{container},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	kubecli := kubernetes.NewForConfigOrDie(t.config)
	_, err := kubecli.Batch().Jobs(t.namespace).Create(testJob)

	return err
}

func (t *BatchJobSample) doDeleteQueueJob(queueJobInfo *QueueJobInfo) error {
	queueJobClient, _, err := client.NewQueueJobClient(t.config)
	if err != nil {
		panic(err)
	}

	err = queueJobClient.Delete().
		Namespace(t.namespace).
		Resource(apiv1.QueueJobPlural).
		Name(queueJobInfo.queueJobName).
		Body(&metav1.DeleteOptions{}).
		Do().
		Error()

	return err
}

func (t *BatchJobSample) findQueueJobInfoByName(name string) *QueueJobInfo {
	for _, v := range t.queueJobToJob {
		if v.queueJobName == name {
			return v
		}
	}
	return nil
}

func (t *BatchJobSample) findQueueJobInfoByJob(name string) *QueueJobInfo {
	for _, v := range t.queueJobToJob {
		if v.jobName == name {
			return v
		}
	}
	return nil
}

func (t *BatchJobSample) changeJobStatus(info *QueueJobInfo, status jobStatus) {
	info.status = status
}

func (t *BatchJobSample) buildConfig(master, kubeconfig string) (*rest.Config, error) {
	if master != "" || kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags(master, kubeconfig)
	}
	return rest.InClusterConfig()
}

func (t *BatchJobSample) flagInitOrDie() {
	pflag.CommandLine.StringVar(&t.namespace, "namespace", "", "The namespaces submit job")
	pflag.CommandLine.StringVar(&t.kubeconfig, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information")
	pflag.CommandLine.StringVar(&t.queue, "queue", "", "The queue name")
	pflag.CommandLine.Int32Var(&t.taskno, "taskno", 4, "The task number in each job")
	pflag.CommandLine.StringVar(&t.sleeptime, "sleeptime", "60", "The task running time")
	pflag.Parse()
}

func (t *BatchJobSample) initOrDie() {
	t.flagInitOrDie()

	// create kube config first
	config, err := t.buildConfig("", t.kubeconfig)
	if err != nil {
		panic(err)
	}
	t.config = config

	// create 3 queuejob under same namespace
	ts1 := &QueueJobInfo{
		priority:     1,
		queueJobName: t.namespace + "-ts-01",
		jobName:      t.namespace + "-job-01",
		taskNo:       t.taskno,
		status:       JobStatusPending,
	}
	ts2 := &QueueJobInfo{
		priority:     2,
		queueJobName: t.namespace + "-ts-02",
		jobName:      t.namespace + "-job-02",
		taskNo:       t.taskno,
		status:       JobStatusPending,
	}
	ts3 := &QueueJobInfo{
		priority:     3,
		queueJobName: t.namespace + "-ts-03",
		jobName:      t.namespace + "-job-03",
		taskNo:       t.taskno,
		status:       JobStatusPending,
	}
	t.queueJobToJob = []*QueueJobInfo{
		ts1,
		ts2,
		ts3,
	}
	queueJobClient, _, err := client.NewQueueJobClient(config)
	if err != nil {
		panic(err)
	}
	for _, info := range t.queueJobToJob {
		ts := &apiv1.QueueJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      info.queueJobName,
				Namespace: t.namespace,
			},
			Spec: apiv1.QueueJobSpec{
				Queue:      t.queue,
				Priority:   info.priority,
				ResourceNo: int(info.taskNo),
				ResourceUnit: apiv1.ResourceList{
					Resources: map[apiv1.ResourceName]resource.Quantity{
						"cpu":    resource.MustParse("1"),
						"memory": resource.MustParse("1Gi"),
					},
				},
			},
		}

		var result apiv1.QueueJob
		err = queueJobClient.Post().
			Resource(apiv1.QueueJobPlural).
			Namespace(ts.Namespace).
			Body(ts).
			Do().Into(&result)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			panic(err)
		}
	}

	t.printQueueJobStatus()
}

func (t *BatchJobSample) AddQueueJob(obj interface{}) {
	queuejob, ok := obj.(*apiv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert to *apiv1.QueueJob: %v", obj)
		return
	}
	glog.V(4).Infof("====== queuejob %s is added\n", queuejob.Name)
}

func (t *BatchJobSample) UpdateQueueJob(oldObj, newObj interface{}) {
	oldQueueJob, ok := oldObj.(*apiv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *apiv1.QueueJob: %v", oldObj)
		return
	}
	newQueueJob, ok := newObj.(*apiv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert newObj to *apiv1.QueueJob: %v", newObj)
		return
	}

	glog.V(4).Infof("queuejob is updated, old %s, new %s\n", oldQueueJob.Name, newQueueJob.Name)
	info := t.findQueueJobInfoByName(newQueueJob.Name)
	if info != nil && info.status == JobStatusPending && newQueueJob.Status.Allocated.Resources != nil {
		cpuRes := newQueueJob.Status.Allocated.Resources["cpu"]
		memRes := newQueueJob.Status.Allocated.Resources["memory"]
		cpuInt, _ := cpuRes.AsInt64()
		memInt, _ := memRes.AsInt64()
		if info != nil && cpuInt >= int64(info.taskNo) {
			time.Sleep(5 * time.Second)
			err := t.doCreateJob(info)
			t.changeJobStatus(info, JobStatusRunning)
			if err != nil {
				fmt.Printf("====== Fail to create job %s, %#v\n", info.jobName, err)
			} else {
				fmt.Printf("----> queuejob <%s> get enough resources cpu <%d> memory <%d>, then submit job <%s> to cluster\n", info.queueJobName, cpuInt, memInt, info.jobName)
				t.printQueueJobStatus()
			}
		}
	}
}

func (t *BatchJobSample) DeleteQueueJob(obj interface{}) {
	queuejob, ok := obj.(*apiv1.QueueJob)
	if !ok {
		glog.Errorf("Cannot convert to *apiv1.QueueJob: %v", obj)
		return
	}
	glog.V(4).Infof("====== queuejob %s is delete\n", queuejob.Name)
}

func (t *BatchJobSample) AddJob(obj interface{}) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		glog.Errorf("Cannot convert to *batchv1.Job: %v", obj)
		return
	}
	glog.V(4).Infof("====== job %s is added\n", job.Name)
}

func (t *BatchJobSample) UpdateJob(oldObj, newObj interface{}) {
	oldJob, ok := oldObj.(*batchv1.Job)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *batchv1.Job: %v", oldObj)
		return
	}
	newJob, ok := newObj.(*batchv1.Job)
	if !ok {
		glog.Errorf("Cannot convert newObj to *batchv1.Job: %v", newObj)
		return
	}

	glog.V(4).Infof("====== job is updated, old %s, new %s\n", oldJob.Name, newJob.Name)

	newConditions := newJob.Status.Conditions
	for _, con := range newConditions {
		if con.Type == batchv1.JobComplete && con.Status == corev1.ConditionTrue {
			info := t.findQueueJobInfoByJob(newJob.Name)
			if info != nil {
				t.changeJobStatus(info, JobStatusDone)
				err := t.doDeleteQueueJob(info)
				if err != nil {
					fmt.Printf("====== Fail to delete queuejob %s, %#v\n", info.queueJobName, err)
				} else {
					fmt.Printf("----> Job <%s> is done, then delete queuejob %s from cluster to release resources\n", info.jobName, info.queueJobName)
				}
				t.printQueueJobStatus()
			}
		}
	}
}

func (t *BatchJobSample) DeleteJob(obj interface{}) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		glog.Errorf("Cannot convert to *batchv1.Job: %v", obj)
		return
	}
	glog.V(4).Infof("====== job %s is delete\n", job.Name)
}

func (t *BatchJobSample) Run(stopCh <-chan struct{}) {
	queueJobClient, _, err := client.NewQueueJobClient(t.config)
	if err != nil {
		panic(err)
	}
	tsInformerFactory := qInformerfactory.NewSharedInformerFactory(queueJobClient, 0)
	// create informer for queuejob
	queueJobInformer := tsInformerFactory.QueueJob().QueueJobs()
	queueJobInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *apiv1.QueueJob:
					glog.V(4).Infof("Filter queuejob name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    t.AddQueueJob,
				UpdateFunc: t.UpdateQueueJob,
				DeleteFunc: t.DeleteQueueJob,
			},
		})
	// create informer for job
	kubecli := kubernetes.NewForConfigOrDie(t.config)
	informerFactory := informers.NewSharedInformerFactory(kubecli, 0)
	jobInformer := informerFactory.Batch().V1().Jobs()
	jobInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *batchv1.Job:
					glog.V(4).Infof("Filter job name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    t.AddJob,
				UpdateFunc: t.UpdateJob,
				DeleteFunc: t.DeleteJob,
			},
		},
	)

	go queueJobInformer.Informer().Run(stopCh)
	go jobInformer.Informer().Run(stopCh)
}

func main() {
	testObj := &BatchJobSample{}

	neverStop := make(chan struct{})
	testObj.initOrDie()
	testObj.Run(neverStop)

	<-neverStop
}
