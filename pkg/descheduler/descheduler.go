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

Copyright 2024 The Volcano Authors.

Modifications made by Volcano authors:
- [2024]Support crontab expression running descheduler
*/

package descheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	v1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	listersv1 "k8s.io/client-go/listers/core/v1"
	schedulingv1 "k8s.io/client-go/listers/scheduling/v1"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/events"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/klog/v2"
	"sigs.k8s.io/descheduler/metrics"
	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/descheduler/client"
	"sigs.k8s.io/descheduler/pkg/descheduler/evictions"
	eutils "sigs.k8s.io/descheduler/pkg/descheduler/evictions/utils"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	"sigs.k8s.io/descheduler/pkg/framework/pluginregistry"
	"sigs.k8s.io/descheduler/pkg/utils"
	"sigs.k8s.io/descheduler/pkg/version"

	"volcano.sh/volcano/cmd/descheduler/app/options"
	frameworkprofile "volcano.sh/volcano/pkg/descheduler/framework/profile"
)

const podNameEnvKey string = "HOSTNAME"
const podNamespaceEnvKey string = "POD_NAMESPACE"

func Run(ctx context.Context, rs *options.DeschedulerServer) error {
	metrics.Register()

	clientConnection := rs.ClientConnection
	if rs.KubeconfigFile != "" && clientConnection.Kubeconfig == "" {
		clientConnection.Kubeconfig = rs.KubeconfigFile
	}

	rsclient, eventClient, err := createClients(clientConnection)
	if err != nil {
		return err
	}
	rs.Client = rsclient
	rs.EventClient = eventClient

	deschedulerPolicy, err := LoadPolicyConfig(rs.PolicyConfigFile, rs.Client, pluginregistry.PluginRegistry)
	if err != nil {
		return err
	}
	if deschedulerPolicy == nil {
		return fmt.Errorf("deschedulerPolicy is nil")
	}
	deschedulerPolicyJson, err := json.MarshalIndent(deschedulerPolicy, "", "  ")
	if err != nil {
		return fmt.Errorf("faild to marshal deschedulerPolicy: %v", err)
	}
	klog.V(5).InfoS("Successfully load descheduler policy", "deschedulerPolicy", string(deschedulerPolicyJson))

	// Add k8s compatibility warnings to logs
	versionCompatibilityCheck(rs)

	evictionPolicyGroupVersion, err := eutils.SupportEviction(rs.Client)
	if err != nil || len(evictionPolicyGroupVersion) == 0 {
		return err
	}

	runFn := func() error {
		return RunDeschedulerStrategies(ctx, rs, deschedulerPolicy, evictionPolicyGroupVersion)
	}

	if rs.DeschedulingInterval.Seconds() == 0 {
		_, err = cron.ParseStandard(rs.DeschedulingIntervalCronExpression)
		if rs.DeschedulingIntervalCronExpression == "" || err != nil {
			return fmt.Errorf("Both DeschedulingInterval and deschedulingIntervalCronExpression are not configured. At least one of these two parameter must be configured. ")
		}
		klog.V(1).InfoS("Run with deschedulingIntervalCronExpression", "intervalCronExpression", rs.DeschedulingIntervalCronExpression)
	} else {
		klog.V(1).InfoS("Run with deschedulingInterval", "interval", rs.DeschedulingInterval)
	}

	if rs.LeaderElection.LeaderElect && rs.DryRun {
		klog.V(1).InfoS("Warning: DryRun is set to True. You need to disable it to use Leader Election.")
	}

	if rs.LeaderElection.LeaderElect && !rs.DryRun {
		if err := NewLeaderElection(runFn, rsclient, &rs.LeaderElection, ctx); err != nil {
			return fmt.Errorf("leaderElection: %w", err)
		}
		return nil
	}

	return runFn()
}

func versionCompatibilityCheck(rs *options.DeschedulerServer) {
	serverVersion, serverErr := rs.Client.Discovery().ServerVersion()
	if serverErr != nil {
		klog.V(1).InfoS("Warning: Get Kubernetes server version fail")
		return
	}

	deschedulerMinorVersion := strings.Split(version.Get().Minor, ".")[0]
	deschedulerMinorVersionFloat, err := strconv.ParseFloat(deschedulerMinorVersion, 64)
	if err != nil {
		klog.Warning("Warning: Convert Descheduler minor version to float fail")
	}

	kubernetesMinorVersionFloat, err := strconv.ParseFloat(serverVersion.Minor, 64)
	if err != nil {
		klog.Warning("Warning: Convert Kubernetes server minor version to float fail")
	}

	if math.Abs(deschedulerMinorVersionFloat-kubernetesMinorVersionFloat) > 3 {
		klog.Warningf("Warning: Descheduler minor version %v is not supported on your version of Kubernetes %v.%v. See compatibility docs for more info: https://github.com/kubernetes-sigs/descheduler#compatibility-matrix", deschedulerMinorVersion, serverVersion.Major, serverVersion.Minor)
	}
}

func cachedClient(
	realClient clientset.Interface,
	podLister listersv1.PodLister,
	nodeLister listersv1.NodeLister,
	namespaceLister listersv1.NamespaceLister,
	priorityClassLister schedulingv1.PriorityClassLister,
) (clientset.Interface, error) {
	fakeClient := fakeclientset.NewSimpleClientset()
	// simulate a pod eviction by deleting a pod
	fakeClient.PrependReactor("create", "pods", func(action core.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "eviction" {
			createAct, matched := action.(core.CreateActionImpl)
			if !matched {
				return false, nil, fmt.Errorf("unable to convert action to core.CreateActionImpl")
			}
			eviction, matched := createAct.Object.(*policy.Eviction)
			if !matched {
				return false, nil, fmt.Errorf("unable to convert action object into *policy.Eviction")
			}
			if err := fakeClient.Tracker().Delete(action.GetResource(), eviction.GetNamespace(), eviction.GetName()); err != nil {
				return false, nil, fmt.Errorf("unable to delete pod %v/%v: %v", eviction.GetNamespace(), eviction.GetName(), err)
			}
			return true, nil, nil
		}
		// fallback to the default reactor
		return false, nil, nil
	})

	klog.V(3).Infof("Pulling resources for the cached client from the cluster")
	pods, err := podLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("unable to list pods: %v", err)
	}

	for _, item := range pods {
		if _, err := fakeClient.CoreV1().Pods(item.Namespace).Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy pod: %v", err)
		}
	}

	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("unable to list nodes: %v", err)
	}

	for _, item := range nodes {
		if _, err := fakeClient.CoreV1().Nodes().Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy node: %v", err)
		}
	}

	namespaces, err := namespaceLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("unable to list namespaces: %v", err)
	}

	for _, item := range namespaces {
		if _, err := fakeClient.CoreV1().Namespaces().Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy namespace: %v", err)
		}
	}

	priorityClasses, err := priorityClassLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("unable to list priorityclasses: %v", err)
	}

	for _, item := range priorityClasses {
		if _, err := fakeClient.SchedulingV1().PriorityClasses().Create(context.TODO(), item, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("unable to copy priorityclass: %v", err)
		}
	}

	return fakeClient, nil
}

func RunDeschedulerStrategies(ctx context.Context, rs *options.DeschedulerServer, deschedulerPolicy *api.DeschedulerPolicy, evictionPolicyGroupVersion string) error {
	sharedInformerFactory := informers.NewSharedInformerFactory(rs.Client, 0)
	podInformer := sharedInformerFactory.Core().V1().Pods().Informer()
	podLister := sharedInformerFactory.Core().V1().Pods().Lister()
	nodeLister := sharedInformerFactory.Core().V1().Nodes().Lister()
	namespaceLister := sharedInformerFactory.Core().V1().Namespaces().Lister()
	priorityClassLister := sharedInformerFactory.Scheduling().V1().PriorityClasses().Lister()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	getPodsAssignedToNode, err := podutil.BuildGetPodsAssignedToNodeFunc(podInformer)
	if err != nil {
		return fmt.Errorf("build get pods assigned to node function error: %v", err)
	}

	sharedInformerFactory.Start(ctx.Done())
	sharedInformerFactory.WaitForCacheSync(ctx.Done())

	var nodeSelector string
	if deschedulerPolicy.NodeSelector != nil {
		nodeSelector = *deschedulerPolicy.NodeSelector
	}

	var eventClient clientset.Interface
	if rs.DryRun {
		eventClient = fakeclientset.NewSimpleClientset()
	} else {
		eventClient = rs.Client
	}

	eventBroadcaster, eventRecorder := utils.GetRecorderAndBroadcaster(ctx, eventClient)
	defer eventBroadcaster.Shutdown()

	cycleSharedInformerFactory := sharedInformerFactory
	run := func() {
		deschedulerPolicy, err = LoadPolicyConfig(rs.PolicyConfigFile, rs.Client, pluginregistry.PluginRegistry)
		if err != nil || deschedulerPolicy == nil {
			klog.ErrorS(err, "Failed to load policy config")
			eventDeschedulerParamErr(rs.Client, eventRecorder, err)
			return
		}

		deschedulerPolicyJson, err := json.MarshalIndent(deschedulerPolicy, "", "  ")
		if err != nil {
			klog.ErrorS(err, "Failed to marshal descheduler policy")
			return
		}
		klog.V(5).InfoS("Successfully load descheduler policy", "deschedulerPolicy", string(deschedulerPolicyJson))

		loopStartDuration := time.Now()
		defer metrics.DeschedulerLoopDuration.With(map[string]string{}).Observe(time.Since(loopStartDuration).Seconds())
		nodes, err := nodeutil.ReadyNodes(ctx, rs.Client, nodeLister, nodeSelector)
		if err != nil {
			klog.V(1).InfoS("Unable to get ready nodes", "err", err)
			cancel()
			return
		}

		if len(nodes) <= 1 {
			klog.V(1).InfoS("The cluster size is 0 or 1 meaning eviction causes service disruption or degradation. So aborting..")
			cancel()
			return
		}

		var client clientset.Interface
		// When the dry mode is enable, collect all the relevant objects (mostly pods) under a fake client.
		// So when evicting pods while running multiple strategies in a row have the cummulative effect
		// as is when evicting pods for real.
		if rs.DryRun {
			klog.V(3).Infof("Building a cached client from the cluster for the dry run")
			// Create a new cache so we start from scratch without any leftovers
			fakeClient, err := cachedClient(rs.Client, podLister, nodeLister, namespaceLister, priorityClassLister)
			if err != nil {
				klog.Error(err)
				return
			}

			// create a new instance of the shared informer factor from the cached client
			fakeSharedInformerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
			// register the pod informer, otherwise it will not get running
			getPodsAssignedToNode, err = podutil.BuildGetPodsAssignedToNodeFunc(fakeSharedInformerFactory.Core().V1().Pods().Informer())
			if err != nil {
				klog.Errorf("build get pods assigned to node function error: %v", err)
				return
			}

			fakeCtx, cncl := context.WithCancel(context.TODO())
			defer cncl()
			fakeSharedInformerFactory.Start(fakeCtx.Done())
			fakeSharedInformerFactory.WaitForCacheSync(fakeCtx.Done())

			client = fakeClient
			cycleSharedInformerFactory = fakeSharedInformerFactory
		} else {
			client = rs.Client
		}

		klog.V(3).Infof("Building a pod evictor")
		podEvictor := evictions.NewPodEvictor(
			client,
			evictionPolicyGroupVersion,
			rs.DryRun,
			deschedulerPolicy.MaxNoOfPodsToEvictPerNode,
			deschedulerPolicy.MaxNoOfPodsToEvictPerNamespace,
			nodes,
			!rs.DisableMetrics,
			eventRecorder,
		)

		for _, profile := range deschedulerPolicy.Profiles {
			currProfile, err := frameworkprofile.NewProfile(
				profile,
				pluginregistry.PluginRegistry,
				frameworkprofile.WithClientSet(client),
				frameworkprofile.WithSharedInformerFactory(cycleSharedInformerFactory),
				frameworkprofile.WithPodEvictor(podEvictor),
				frameworkprofile.WithGetPodsAssignedToNodeFnc(getPodsAssignedToNode),
			)
			if err != nil {
				klog.ErrorS(err, "unable to create a profile", "profile", profile.Name)
				continue
			}

			// First deschedule
			status := currProfile.RunDeschedulePlugins(ctx, nodes)
			if status != nil && status.Err != nil {
				klog.ErrorS(status.Err, "running deschedule extension point failed with error", "profile", profile.Name)
				continue
			}
			// Then balance
			status = currProfile.RunBalancePlugins(ctx, nodes)
			if status != nil && status.Err != nil {
				klog.ErrorS(status.Err, "running balance extension point failed with error", "profile", profile.Name)
				continue
			}
		}

		klog.V(1).InfoS("Number of evicted pods", "totalEvicted", podEvictor.TotalEvicted())
	}
	if rs.DeschedulingInterval.Seconds() != 0 {
		wait.NonSlidingUntil(run, rs.DeschedulingInterval, ctx.Done())
	} else {
		c := cron.New()
		c.AddFunc(rs.DeschedulingIntervalCronExpression, run)
		c.Run()
	}
	return nil
}

func GetPluginConfig(pluginName string, pluginConfigs []api.PluginConfig) (*api.PluginConfig, int) {
	for idx, pluginConfig := range pluginConfigs {
		if pluginConfig.Name == pluginName {
			return &pluginConfig, idx
		}
	}
	return nil, 0
}

func createClients(clientConnection componentbaseconfig.ClientConnectionConfiguration) (clientset.Interface, clientset.Interface, error) {
	kClient, err := client.CreateClient(clientConnection, "descheduler")
	if err != nil {
		return nil, nil, err
	}

	eventClient, err := client.CreateClient(clientConnection, "")
	if err != nil {
		return nil, nil, err
	}

	return kClient, eventClient, nil
}

func eventDeschedulerParamErr(client clientset.Interface, eventRecorder events.EventRecorder, errParam error) {
	podName := os.Getenv(podNameEnvKey)
	podNamespace := os.Getenv(podNamespaceEnvKey)
	pod, err := client.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Want to event error %v,but get pod error %v", errParam, err)
		return
	}
	eventRecorder.Eventf(pod, nil, v1.EventTypeWarning, "Load Config Error", "Warning", "descheduler run err due to parameter error:%v", errParam)
}
