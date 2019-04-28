/*
Copyright 2019 The Kubernetes Authors.

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

package e2e

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	con "context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	imageutils "k8s.io/kubernetes/test/utils/image"
)

const (
	MinPodStartupMeasurements = 30
	TotalPodCount             = 100
)

var _ = Describe("Job E2E Test", func() {
	It("[Feature:Performance] Schedule Density Job", func() {
		if getkubemarkConfigPath() == "" {
			Skip("Performance test skipped since config file not found")
		}
		context := initKubemarkDensityTestContext()
		defer cleanupDensityTestContext(context)

		_, pg := createDensityJob(context, &jobSpec{
			name: "qj-1",
			tasks: []taskSpec{
				{
					img: "busybox",
					req: smallCPU,
					min: TotalPodCount,
					rep: TotalPodCount,
				},
			},
		})

		err := waitDensityTasksReady(context, pg, TotalPodCount)
		checkError(context, err)

		nodeCount := 0
		missingMeasurements := 0
		nodes := getAllWorkerNodes(context)
		nodeCount = len(nodes)

		latencyPodsIterations := (MinPodStartupMeasurements + nodeCount - 1) / nodeCount
		By(fmt.Sprintf("Scheduling additional %d Pods to measure startup latencies", latencyPodsIterations*nodeCount))

		createTimes := make(map[string]metav1.Time, 0)
		nodeNames := make(map[string]string, 0)
		scheduleTimes := make(map[string]metav1.Time, 0)
		runTimes := make(map[string]metav1.Time, 0)
		watchTimes := make(map[string]metav1.Time, 0)

		var mutex sync.Mutex
		checkPod := func(p *v1.Pod) {
			mutex.Lock()
			defer mutex.Unlock()
			defer GinkgoRecover()

			if p.Status.Phase == v1.PodRunning {
				if _, found := watchTimes[p.Name]; !found {
					watchTimes[p.Name] = metav1.Now()
					createTimes[p.Name] = p.CreationTimestamp
					nodeNames[p.Name] = p.Spec.NodeName
					var startTime metav1.Time
					for _, cs := range p.Status.ContainerStatuses {
						if cs.State.Running != nil {
							if startTime.Before(&cs.State.Running.StartedAt) {
								startTime = cs.State.Running.StartedAt
							}
						}
					}
					if startTime != metav1.NewTime(time.Time{}) {
						runTimes[p.Name] = startTime
					} else {
						fmt.Println("Pod  is reported to be running, but none of its containers is", p.Name)
					}
				}
			}
		}

		additionalPodsPrefix := "density-latency-pod"
		stopCh := make(chan struct{})

		nsName := context.namespace
		_, controller := cache.NewInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					options.LabelSelector = labels.SelectorFromSet(labels.Set{"type": additionalPodsPrefix}).String()
					obj, err := context.kubeclient.CoreV1().Pods(nsName).List(options)
					return runtime.Object(obj), err
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					options.LabelSelector = labels.SelectorFromSet(labels.Set{"type": additionalPodsPrefix}).String()
					return context.kubeclient.CoreV1().Pods(nsName).Watch(options)
				},
			},
			&v1.Pod{},
			0,
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					p, ok := obj.(*v1.Pod)
					if !ok {
						fmt.Println("Failed to cast observed object to *v1.Pod.")
					}
					Expect(ok).To(Equal(true))
					go checkPod(p)
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					p, ok := newObj.(*v1.Pod)
					if !ok {
						fmt.Println("Failed to cast observed object to *v1.Pod.")
					}
					Expect(ok).To(Equal(true))
					go checkPod(p)
				},
			},
		)

		go controller.Run(stopCh)

		for latencyPodsIteration := 0; latencyPodsIteration < latencyPodsIterations; latencyPodsIteration++ {
			podIndexOffset := latencyPodsIteration * nodeCount
			fmt.Println("Creating  latency pods in range ", nodeCount, podIndexOffset+1, podIndexOffset+nodeCount)

			watchTimesLen := len(watchTimes)

			var wg sync.WaitGroup
			wg.Add(nodeCount)

			cpuRequest := *resource.NewMilliQuantity(1, resource.DecimalSI)
			memRequest := *resource.NewQuantity(1, resource.DecimalSI)

			rcNameToNsMap := map[string]string{}
			for i := 1; i <= nodeCount; i++ {
				name := additionalPodsPrefix + "-" + strconv.Itoa(podIndexOffset+i)
				nsName := context.namespace
				rcNameToNsMap[name] = nsName
				go createRunningPodFromRC(&wg, context, name, imageutils.GetPauseImageName(), additionalPodsPrefix, cpuRequest, memRequest)
				time.Sleep(200 * time.Millisecond)
			}
			wg.Wait()

			By("Waiting for all Pods begin observed by the watch...")
			waitTimeout := 10 * time.Minute
			for start := time.Now(); len(watchTimes) < watchTimesLen+nodeCount; time.Sleep(10 * time.Second) {
				if time.Since(start) < waitTimeout {
					fmt.Println("Timeout reached waiting for all Pods being observed by the watch.")
				}
			}

			By("Removing additional replication controllers")
			deleteRC := func(i int) {
				defer GinkgoRecover()
				name := additionalPodsPrefix + "-" + strconv.Itoa(podIndexOffset+i+1)
				deleteReplicationController(context, name)
			}
			workqueue.ParallelizeUntil(con.TODO(), 25, nodeCount, deleteRC)
		}
		close(stopCh)

		nsName = context.namespace
		//time.Sleep(1 * time.Minute) // sleep to be added for large number of pods
		selector := fields.Set{
			"involvedObject.kind":      "Pod",
			"involvedObject.namespace": nsName,
			"source":                   "kube-batch",
		}.AsSelector().String()
		options := metav1.ListOptions{FieldSelector: selector}
		schedEvents, _ := context.kubeclient.CoreV1().Events(nsName).List(options)
		for k := range createTimes {
			for _, event := range schedEvents.Items {
				if event.InvolvedObject.Name == k {
					scheduleTimes[k] = event.FirstTimestamp
					break
				}
			}
		}

		scheduleLag := make([]PodLatencyData, 0)
		startupLag := make([]PodLatencyData, 0)
		watchLag := make([]PodLatencyData, 0)
		schedToWatchLag := make([]PodLatencyData, 0)
		e2eLag := make([]PodLatencyData, 0)

		for name, create := range createTimes {
			sched, ok := scheduleTimes[name]
			if !ok {
				fmt.Println("Failed to find schedule time for ", name)
				missingMeasurements++
			}
			run, ok := runTimes[name]
			if !ok {
				fmt.Println("Failed to find run time for", name)
				missingMeasurements++
			}
			watch, ok := watchTimes[name]
			if !ok {
				fmt.Println("Failed to find watch time for", name)
				missingMeasurements++
			}
			node, ok := nodeNames[name]
			if !ok {
				fmt.Println("Failed to find node for", name)
				missingMeasurements++
			}
			scheduleLag = append(scheduleLag, PodLatencyData{Name: name, Node: node, Latency: sched.Time.Sub(create.Time)})
			startupLag = append(startupLag, PodLatencyData{Name: name, Node: node, Latency: run.Time.Sub(sched.Time)})
			watchLag = append(watchLag, PodLatencyData{Name: name, Node: node, Latency: watch.Time.Sub(run.Time)})
			schedToWatchLag = append(schedToWatchLag, PodLatencyData{Name: name, Node: node, Latency: watch.Time.Sub(sched.Time)})
			e2eLag = append(e2eLag, PodLatencyData{Name: name, Node: node, Latency: watch.Time.Sub(create.Time)})
		}

		sort.Sort(LatencySlice(scheduleLag))
		sort.Sort(LatencySlice(startupLag))
		sort.Sort(LatencySlice(watchLag))
		sort.Sort(LatencySlice(schedToWatchLag))
		sort.Sort(LatencySlice(e2eLag))

		PrintLatencies(scheduleLag, "worst create-to-schedule latencies")
		PrintLatencies(startupLag, "worst schedule-to-run latencies")
		PrintLatencies(watchLag, "worst run-to-watch latencies")
		PrintLatencies(schedToWatchLag, "worst schedule-to-watch latencies")
		PrintLatencies(e2eLag, "worst e2e latencies")

		//// Capture latency metrics related to pod-startup.
		podStartupLatency := &PodStartupLatency{
			CreateToScheduleLatency: ExtractLatencyMetrics(scheduleLag),
			ScheduleToRunLatency:    ExtractLatencyMetrics(startupLag),
			RunToWatchLatency:       ExtractLatencyMetrics(watchLag),
			ScheduleToWatchLatency:  ExtractLatencyMetrics(schedToWatchLag),
			E2ELatency:              ExtractLatencyMetrics(e2eLag),
		}

		fmt.Println(podStartupLatency.PrintJSON())

		dir, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(dir)

		filePath := path.Join(dir, "MetricsForE2ESuite_"+time.Now().Format(time.RFC3339)+".json")
		if err := ioutil.WriteFile(filePath, []byte(podStartupLatency.PrintJSON()), 0644); err != nil {
			fmt.Errorf("error writing to %q: %v", filePath, err)
		}

	})
})
