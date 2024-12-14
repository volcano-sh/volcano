package profile

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/descheduler/evictions"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	fakeplugin "sigs.k8s.io/descheduler/pkg/framework/fake/plugin"
	"sigs.k8s.io/descheduler/pkg/framework/pluginregistry"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/defaultevictor"
	frameworktypes "sigs.k8s.io/descheduler/pkg/framework/types"
	"sigs.k8s.io/descheduler/pkg/utils"
	testutils "sigs.k8s.io/descheduler/test"
)

func TestProfileDescheduleBalanceExtensionPointsEviction(t *testing.T) {
	tests := []struct {
		name             string
		config           api.DeschedulerProfile
		extensionPoint   frameworktypes.ExtensionPoint
		expectedEviction bool
	}{
		{
			name: "profile with deschedule extension point enabled single eviction",
			config: api.DeschedulerProfile{
				Name: "strategy-test-profile-with-deschedule",
				PluginConfigs: []api.PluginConfig{
					{
						Name: defaultevictor.PluginName,
						Args: &defaultevictor.DefaultEvictorArgs{
							PriorityThreshold: &api.PriorityThreshold{
								Value: nil,
							},
						},
					},
					{
						Name: "FakePlugin",
						Args: &fakeplugin.FakePluginArgs{},
					},
				},
				Plugins: api.Plugins{
					Deschedule: api.PluginSet{
						Enabled: []string{"FakePlugin"},
					},
					Filter: api.PluginSet{
						Enabled: []string{defaultevictor.PluginName},
					},
					PreEvictionFilter: api.PluginSet{
						Enabled: []string{defaultevictor.PluginName},
					},
				},
			},
			extensionPoint:   frameworktypes.DescheduleExtensionPoint,
			expectedEviction: true,
		},
		{
			name: "profile with balance extension point enabled single eviction",
			config: api.DeschedulerProfile{
				Name: "strategy-test-profile-with-balance",
				PluginConfigs: []api.PluginConfig{
					{
						Name: defaultevictor.PluginName,
						Args: &defaultevictor.DefaultEvictorArgs{
							PriorityThreshold: &api.PriorityThreshold{
								Value: nil,
							},
						},
					},
					{
						Name: "FakePlugin",
						Args: &fakeplugin.FakePluginArgs{},
					},
				},
				Plugins: api.Plugins{
					Balance: api.PluginSet{
						Enabled: []string{"FakePlugin"},
					},
					Filter: api.PluginSet{
						Enabled: []string{defaultevictor.PluginName},
					},
					PreEvictionFilter: api.PluginSet{
						Enabled: []string{defaultevictor.PluginName},
					},
				},
			},
			extensionPoint:   frameworktypes.BalanceExtensionPoint,
			expectedEviction: true,
		},
		{
			name: "profile with deschedule extension point balance enabled no eviction",
			config: api.DeschedulerProfile{
				Name: "strategy-test-profile-with-deschedule",
				PluginConfigs: []api.PluginConfig{
					{
						Name: defaultevictor.PluginName,
						Args: &defaultevictor.DefaultEvictorArgs{
							PriorityThreshold: &api.PriorityThreshold{
								Value: nil,
							},
						},
					},
					{
						Name: "FakePlugin",
						Args: &fakeplugin.FakePluginArgs{},
					},
				},
				Plugins: api.Plugins{
					Balance: api.PluginSet{
						Enabled: []string{"FakePlugin"},
					},
					Filter: api.PluginSet{
						Enabled: []string{defaultevictor.PluginName},
					},
					PreEvictionFilter: api.PluginSet{
						Enabled: []string{defaultevictor.PluginName},
					},
				},
			},
			extensionPoint:   frameworktypes.DescheduleExtensionPoint,
			expectedEviction: false,
		},
		{
			name: "profile with balance extension point deschedule enabled no eviction",
			config: api.DeschedulerProfile{
				Name: "strategy-test-profile-with-deschedule",
				PluginConfigs: []api.PluginConfig{
					{
						Name: defaultevictor.PluginName,
						Args: &defaultevictor.DefaultEvictorArgs{
							PriorityThreshold: &api.PriorityThreshold{
								Value: nil,
							},
						},
					},
					{
						Name: "FakePlugin",
						Args: &fakeplugin.FakePluginArgs{},
					},
				},
				Plugins: api.Plugins{
					Deschedule: api.PluginSet{
						Enabled: []string{"FakePlugin"},
					},
					Filter: api.PluginSet{
						Enabled: []string{defaultevictor.PluginName},
					},
					PreEvictionFilter: api.PluginSet{
						Enabled: []string{defaultevictor.PluginName},
					},
				},
			},
			extensionPoint:   frameworktypes.BalanceExtensionPoint,
			expectedEviction: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			n1 := testutils.BuildTestNode("n1", 2000, 3000, 10, nil)
			n2 := testutils.BuildTestNode("n2", 2000, 3000, 10, nil)
			nodes := []*v1.Node{n1, n2}

			p1 := testutils.BuildTestPod(fmt.Sprintf("pod_1_%s", n1.Name), 200, 0, n1.Name, nil)
			p1.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{}}

			fakePlugin := fakeplugin.FakePlugin{}
			if test.extensionPoint == frameworktypes.DescheduleExtensionPoint {
				fakePlugin.AddReactor(string(frameworktypes.DescheduleExtensionPoint), func(action fakeplugin.Action) (handled, filter bool, err error) {
					if dAction, ok := action.(fakeplugin.DescheduleAction); ok {
						if dAction.Handle().Evictor().Evict(ctx, p1, evictions.EvictOptions{}) {
							return true, false, nil
						}
						return true, false, fmt.Errorf("pod not evicted")
					}
					return false, false, nil
				})
			}
			if test.extensionPoint == frameworktypes.BalanceExtensionPoint {
				fakePlugin.AddReactor(string(frameworktypes.BalanceExtensionPoint), func(action fakeplugin.Action) (handled, filter bool, err error) {
					if dAction, ok := action.(fakeplugin.BalanceAction); ok {
						if dAction.Handle().Evictor().Evict(ctx, p1, evictions.EvictOptions{}) {
							return true, false, nil
						}
						return true, false, fmt.Errorf("pod not evicted")
					}
					return false, false, nil
				})
			}

			pluginregistry.PluginRegistry = pluginregistry.NewRegistry()
			pluginregistry.Register(
				"FakePlugin",
				fakeplugin.NewPluginFncFromFake(&fakePlugin),
				&fakeplugin.FakePlugin{},
				&fakeplugin.FakePluginArgs{},
				fakeplugin.ValidateFakePluginArgs,
				fakeplugin.SetDefaults_FakePluginArgs,
				pluginregistry.PluginRegistry,
			)

			pluginregistry.Register(
				defaultevictor.PluginName,
				defaultevictor.New,
				&defaultevictor.DefaultEvictor{},
				&defaultevictor.DefaultEvictorArgs{},
				defaultevictor.ValidateDefaultEvictorArgs,
				defaultevictor.SetDefaults_DefaultEvictorArgs,
				pluginregistry.PluginRegistry,
			)

			client := fakeclientset.NewSimpleClientset(n1, n2, p1)
			var evictedPods []string
			client.PrependReactor("create", "pods", podEvictionReactionFuc(&evictedPods))

			sharedInformerFactory := informers.NewSharedInformerFactory(client, 0)
			podInformer := sharedInformerFactory.Core().V1().Pods().Informer()
			getPodsAssignedToNode, err := podutil.BuildGetPodsAssignedToNodeFunc(podInformer)
			if err != nil {
				t.Fatalf("build get pods assigned to node function error: %v", err)
			}

			sharedInformerFactory.Start(ctx.Done())
			sharedInformerFactory.WaitForCacheSync(ctx.Done())

			eventClient := fakeclientset.NewSimpleClientset(n1, n2, p1)
			eventBroadcaster, eventRecorder := utils.GetRecorderAndBroadcaster(ctx, eventClient)
			defer eventBroadcaster.Shutdown()

			podEvictor := evictions.NewPodEvictor(client, "policy/v1", false, nil, nil, nodes, true, eventRecorder)

			prfl, err := NewProfile(
				test.config,
				pluginregistry.PluginRegistry,
				WithClientSet(client),
				WithSharedInformerFactory(sharedInformerFactory),
				WithPodEvictor(podEvictor),
				WithGetPodsAssignedToNodeFnc(getPodsAssignedToNode),
			)
			if err != nil {
				t.Fatalf("unable to create %q profile: %v", test.config.Name, err)
			}

			var status *frameworktypes.Status
			switch test.extensionPoint {
			case frameworktypes.DescheduleExtensionPoint:
				status = prfl.RunDeschedulePlugins(ctx, nodes)
			case frameworktypes.BalanceExtensionPoint:
				status = prfl.RunBalancePlugins(ctx, nodes)
			default:
				t.Fatalf("unknown %q extension point", test.extensionPoint)
			}

			if status == nil {
				t.Fatalf("Unexpected nil status")
			}
			if status.Err != nil {
				t.Fatalf("Expected nil error in status, got %q instead", status.Err)
			}

			if test.expectedEviction && len(evictedPods) < 1 {
				t.Fatalf("Expected eviction, got none")
			}
			if !test.expectedEviction && len(evictedPods) > 0 {
				t.Fatalf("Unexpected eviction, expected none")
			}
		})
	}
}

func podEvictionReactionFuc(evictedPods *[]string) func(action core.Action) (bool, runtime.Object, error) {
	return func(action core.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "eviction" {
			createAct, matched := action.(core.CreateActionImpl)
			if !matched {
				return false, nil, fmt.Errorf("unable to convert action to core.CreateActionImpl")
			}
			if eviction, matched := createAct.Object.(*policy.Eviction); matched {
				*evictedPods = append(*evictedPods, eviction.GetName())
			}
		}
		return false, nil, nil // fallback to the default reactor
	}
}

func TestProfileExtensionPoints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	n1 := testutils.BuildTestNode("n1", 2000, 3000, 10, nil)
	n2 := testutils.BuildTestNode("n2", 2000, 3000, 10, nil)
	nodes := []*v1.Node{n1, n2}

	p1 := testutils.BuildTestPod(fmt.Sprintf("pod_1_%s", n1.Name), 200, 0, n1.Name, nil)
	p1.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{}}

	pluginregistry.PluginRegistry = pluginregistry.NewRegistry()

	for i := 0; i < 3; i++ {
		fakePluginName := fmt.Sprintf("FakePlugin_%v", i)
		deschedulePluginName := fmt.Sprintf("DeschedulePlugin_%v", i)
		balancePluginName := fmt.Sprintf("BalancePlugin_%v", i)
		filterPluginName := fmt.Sprintf("FilterPlugin_%v", i)

		fakePlugin := &fakeplugin.FakePlugin{PluginName: fakePluginName}
		fakeDeschedulePlugin := &fakeplugin.FakeDeschedulePlugin{PluginName: deschedulePluginName}
		fakeBalancePlugin := &fakeplugin.FakeBalancePlugin{PluginName: balancePluginName}
		fakeFilterPlugin := &fakeplugin.FakeFilterPlugin{PluginName: filterPluginName}

		pluginregistry.Register(
			fakePluginName,
			fakeplugin.NewPluginFncFromFake(fakePlugin),
			&fakeplugin.FakePlugin{},
			&fakeplugin.FakePluginArgs{},
			fakeplugin.ValidateFakePluginArgs,
			fakeplugin.SetDefaults_FakePluginArgs,
			pluginregistry.PluginRegistry,
		)

		pluginregistry.Register(
			deschedulePluginName,
			fakeplugin.NewFakeDeschedulePluginFncFromFake(fakeDeschedulePlugin),
			&fakeplugin.FakeDeschedulePlugin{},
			&fakeplugin.FakeDeschedulePluginArgs{},
			fakeplugin.ValidateFakePluginArgs,
			fakeplugin.SetDefaults_FakePluginArgs,
			pluginregistry.PluginRegistry,
		)

		pluginregistry.Register(
			balancePluginName,
			fakeplugin.NewFakeBalancePluginFncFromFake(fakeBalancePlugin),
			&fakeplugin.FakeBalancePlugin{},
			&fakeplugin.FakeBalancePluginArgs{},
			fakeplugin.ValidateFakePluginArgs,
			fakeplugin.SetDefaults_FakePluginArgs,
			pluginregistry.PluginRegistry,
		)

		pluginregistry.Register(
			filterPluginName,
			fakeplugin.NewFakeFilterPluginFncFromFake(fakeFilterPlugin),
			&fakeplugin.FakeFilterPlugin{},
			&fakeplugin.FakeFilterPluginArgs{},
			fakeplugin.ValidateFakePluginArgs,
			fakeplugin.SetDefaults_FakePluginArgs,
			pluginregistry.PluginRegistry,
		)
	}

	pluginregistry.Register(
		defaultevictor.PluginName,
		defaultevictor.New,
		&defaultevictor.DefaultEvictor{},
		&defaultevictor.DefaultEvictorArgs{},
		defaultevictor.ValidateDefaultEvictorArgs,
		defaultevictor.SetDefaults_DefaultEvictorArgs,
		pluginregistry.PluginRegistry,
	)

	client := fakeclientset.NewSimpleClientset(n1, n2, p1)
	var evictedPods []string
	client.PrependReactor("create", "pods", podEvictionReactionFuc(&evictedPods))

	sharedInformerFactory := informers.NewSharedInformerFactory(client, 0)
	podInformer := sharedInformerFactory.Core().V1().Pods().Informer()
	getPodsAssignedToNode, err := podutil.BuildGetPodsAssignedToNodeFunc(podInformer)
	if err != nil {
		t.Fatalf("build get pods assigned to node function error: %v", err)
	}

	sharedInformerFactory.Start(ctx.Done())
	sharedInformerFactory.WaitForCacheSync(ctx.Done())

	eventClient := fakeclientset.NewSimpleClientset(n1, n2, p1)
	eventBroadcaster, eventRecorder := utils.GetRecorderAndBroadcaster(ctx, eventClient)
	defer eventBroadcaster.Shutdown()

	podEvictor := evictions.NewPodEvictor(client, "policy/v1", false, nil, nil, nodes, true, eventRecorder)

	prfl, err := NewProfile(
		api.DeschedulerProfile{
			Name: "strategy-test-profile",
			PluginConfigs: []api.PluginConfig{
				{
					Name: defaultevictor.PluginName,
					Args: &defaultevictor.DefaultEvictorArgs{
						PriorityThreshold: &api.PriorityThreshold{
							Value: nil,
						},
					},
				},
				{
					Name: "FakePlugin_0",
					Args: &fakeplugin.FakePluginArgs{},
				},
				{
					Name: "FilterPlugin_0",
					Args: &fakeplugin.FakeFilterPluginArgs{},
				},
				{
					Name: "FilterPlugin_1",
					Args: &fakeplugin.FakeFilterPluginArgs{},
				},
			},
			Plugins: api.Plugins{
				Deschedule: api.PluginSet{
					Enabled: []string{"FakePlugin_0"},
				},
				Filter: api.PluginSet{
					Enabled: []string{defaultevictor.PluginName, "FilterPlugin_1", "FilterPlugin_0"},
				},
				PreEvictionFilter: api.PluginSet{
					Enabled: []string{defaultevictor.PluginName},
				},
			},
		},
		pluginregistry.PluginRegistry,
		WithClientSet(client),
		WithSharedInformerFactory(sharedInformerFactory),
		WithPodEvictor(podEvictor),
		WithGetPodsAssignedToNodeFnc(getPodsAssignedToNode),
	)
	if err != nil {
		t.Fatalf("unable to create profile: %v", err)
	}

	// Validate the extension points of all registered plugins are properly detected

	diff := cmp.Diff(sets.NewString("DeschedulePlugin_0", "DeschedulePlugin_1", "DeschedulePlugin_2", "FakePlugin_0", "FakePlugin_1", "FakePlugin_2"), prfl.deschedule)
	if diff != "" {
		t.Errorf("check for deschedule failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}
	diff = cmp.Diff(sets.NewString("BalancePlugin_0", "BalancePlugin_1", "BalancePlugin_2", "FakePlugin_0", "FakePlugin_1", "FakePlugin_2"), prfl.balance)
	if diff != "" {
		t.Errorf("check for balance failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}
	diff = cmp.Diff(sets.NewString("DefaultEvictor", "FakePlugin_0", "FakePlugin_1", "FakePlugin_2", "FilterPlugin_0", "FilterPlugin_1", "FilterPlugin_2"), prfl.filter)
	if diff != "" {
		t.Errorf("check for filter failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}
	diff = cmp.Diff(sets.NewString("DefaultEvictor", "FakePlugin_0", "FakePlugin_1", "FakePlugin_2", "FilterPlugin_0", "FilterPlugin_1", "FilterPlugin_2"), prfl.preEvictionFilter)
	if diff != "" {
		t.Errorf("check for preEvictionFilter failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}

	// One deschedule ep enabled
	names := []string{}
	for _, pl := range prfl.deschedulePlugins {
		names = append(names, pl.Name())
	}
	sort.Strings(names)
	diff = cmp.Diff(sets.NewString("FakePlugin_0"), sets.NewString(names...))
	if diff != "" {
		t.Errorf("check for deschedule failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}

	// No balance ep enabled
	names = []string{}
	for _, pl := range prfl.balancePlugins {
		names = append(names, pl.Name())
	}
	sort.Strings(names)
	diff = cmp.Diff(sets.NewString(), sets.NewString(names...))
	if diff != "" {
		t.Errorf("check for balance failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}

	// Two filter eps enabled
	names = []string{}
	for _, pl := range prfl.filterPlugins {
		names = append(names, pl.Name())
	}
	sort.Strings(names)
	diff = cmp.Diff(sets.NewString("DefaultEvictor", "FilterPlugin_0", "FilterPlugin_1"), sets.NewString(names...))
	if diff != "" {
		t.Errorf("check for filter failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}
}

func TestProfileExtensionPointOrdering(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	n1 := testutils.BuildTestNode("n1", 2000, 3000, 10, nil)
	n2 := testutils.BuildTestNode("n2", 2000, 3000, 10, nil)
	nodes := []*v1.Node{n1, n2}

	p1 := testutils.BuildTestPod(fmt.Sprintf("pod_1_%s", n1.Name), 200, 0, n1.Name, nil)
	p1.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{}}

	pluginregistry.PluginRegistry = pluginregistry.NewRegistry()

	filterInvocationOrder := []string{}
	preEvictionFilterInvocationOrder := []string{}
	descheduleInvocationOrder := []string{}
	balanceInvocationOrder := []string{}

	for i := 0; i < 3; i++ {
		pluginName := fmt.Sprintf("Filter_%v", i)
		fakeFilterPlugin := &fakeplugin.FakeFilterPlugin{PluginName: pluginName}
		fakeFilterPlugin.AddReactor(string(frameworktypes.FilterExtensionPoint), func(action fakeplugin.Action) (handled, filter bool, err error) {
			if _, ok := action.(fakeplugin.FilterAction); ok {
				filterInvocationOrder = append(filterInvocationOrder, pluginName+"_filter")
				return true, true, nil
			}
			return false, false, nil
		})

		fakeFilterPlugin.AddReactor(string(frameworktypes.PreEvictionFilterExtensionPoint), func(action fakeplugin.Action) (handled, filter bool, err error) {
			if _, ok := action.(fakeplugin.PreEvictionFilterAction); ok {
				preEvictionFilterInvocationOrder = append(preEvictionFilterInvocationOrder, pluginName+"_preEvictionFilter")
				return true, true, nil
			}
			return false, false, nil
		})

		// plugin implementing Filter extension point
		pluginregistry.Register(
			pluginName,
			fakeplugin.NewFakeFilterPluginFncFromFake(fakeFilterPlugin),
			&fakeplugin.FakeFilterPlugin{},
			&fakeplugin.FakeFilterPluginArgs{},
			fakeplugin.ValidateFakePluginArgs,
			fakeplugin.SetDefaults_FakePluginArgs,
			pluginregistry.PluginRegistry,
		)

		fakePluginName := fmt.Sprintf("FakePlugin_%v", i)
		fakePlugin := fakeplugin.FakePlugin{}
		idx := i
		fakePlugin.AddReactor(string(frameworktypes.DescheduleExtensionPoint), func(action fakeplugin.Action) (handled, filter bool, err error) {
			descheduleInvocationOrder = append(descheduleInvocationOrder, fakePluginName)
			if idx == 0 {
				if dAction, ok := action.(fakeplugin.DescheduleAction); ok {
					// Invoke filters
					dAction.Handle().Evictor().Filter(p1)
					// Invoke pre-eviction filters
					dAction.Handle().Evictor().PreEvictionFilter(p1)
					return true, true, nil
				}
				return false, false, nil
			}
			return true, false, nil
		})

		fakePlugin.AddReactor(string(frameworktypes.BalanceExtensionPoint), func(action fakeplugin.Action) (handled, filter bool, err error) {
			balanceInvocationOrder = append(balanceInvocationOrder, fakePluginName)
			return true, false, nil
		})

		pluginregistry.Register(
			fakePluginName,
			fakeplugin.NewPluginFncFromFake(&fakePlugin),
			&fakeplugin.FakePlugin{},
			&fakeplugin.FakePluginArgs{},
			fakeplugin.ValidateFakePluginArgs,
			fakeplugin.SetDefaults_FakePluginArgs,
			pluginregistry.PluginRegistry,
		)
	}

	pluginregistry.Register(
		defaultevictor.PluginName,
		defaultevictor.New,
		&defaultevictor.DefaultEvictor{},
		&defaultevictor.DefaultEvictorArgs{},
		defaultevictor.ValidateDefaultEvictorArgs,
		defaultevictor.SetDefaults_DefaultEvictorArgs,
		pluginregistry.PluginRegistry,
	)

	client := fakeclientset.NewSimpleClientset(n1, n2, p1)
	var evictedPods []string
	client.PrependReactor("create", "pods", podEvictionReactionFuc(&evictedPods))

	sharedInformerFactory := informers.NewSharedInformerFactory(client, 0)
	podInformer := sharedInformerFactory.Core().V1().Pods().Informer()
	getPodsAssignedToNode, err := podutil.BuildGetPodsAssignedToNodeFunc(podInformer)
	if err != nil {
		t.Fatalf("build get pods assigned to node function error: %v", err)
	}

	sharedInformerFactory.Start(ctx.Done())
	sharedInformerFactory.WaitForCacheSync(ctx.Done())

	eventClient := fakeclientset.NewSimpleClientset(n1, n2, p1)
	eventBroadcaster, eventRecorder := utils.GetRecorderAndBroadcaster(ctx, eventClient)
	defer eventBroadcaster.Shutdown()

	podEvictor := evictions.NewPodEvictor(client, "policy/v1", false, nil, nil, nodes, true, eventRecorder)

	prfl, err := NewProfile(
		api.DeschedulerProfile{
			Name: "strategy-test-profile",
			PluginConfigs: []api.PluginConfig{
				{
					Name: defaultevictor.PluginName,
					Args: &defaultevictor.DefaultEvictorArgs{
						PriorityThreshold: &api.PriorityThreshold{
							Value: nil,
						},
					},
				},
				{
					Name: "FakePlugin_0",
					Args: &fakeplugin.FakePluginArgs{},
				},
				{
					Name: "FakePlugin_1",
					Args: &fakeplugin.FakePluginArgs{},
				},
				{
					Name: "FakePlugin_2",
					Args: &fakeplugin.FakePluginArgs{},
				},
				{
					Name: "Filter_0",
					Args: &fakeplugin.FakeFilterPluginArgs{},
				},
				{
					Name: "Filter_1",
					Args: &fakeplugin.FakeFilterPluginArgs{},
				},
				{
					Name: "Filter_2",
					Args: &fakeplugin.FakeFilterPluginArgs{},
				},
			},
			Plugins: api.Plugins{
				Deschedule: api.PluginSet{
					Enabled: []string{"FakePlugin_2", "FakePlugin_0", "FakePlugin_1"},
				},
				Balance: api.PluginSet{
					Enabled: []string{"FakePlugin_1", "FakePlugin_0", "FakePlugin_2"},
				},
				Filter: api.PluginSet{
					Enabled: []string{defaultevictor.PluginName, "Filter_2", "Filter_1", "Filter_0"},
				},
				PreEvictionFilter: api.PluginSet{
					Enabled: []string{defaultevictor.PluginName, "Filter_2", "Filter_1", "Filter_0"},
				},
			},
		},
		pluginregistry.PluginRegistry,
		WithClientSet(client),
		WithSharedInformerFactory(sharedInformerFactory),
		WithPodEvictor(podEvictor),
		WithGetPodsAssignedToNodeFnc(getPodsAssignedToNode),
	)
	if err != nil {
		t.Fatalf("unable to create profile: %v", err)
	}

	prfl.RunDeschedulePlugins(ctx, nodes)

	diff := cmp.Diff([]string{"Filter_2_filter", "Filter_1_filter", "Filter_0_filter"}, filterInvocationOrder)
	if diff != "" {
		t.Errorf("check for filter invocation order failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}

	diff = cmp.Diff([]string{"Filter_2_preEvictionFilter", "Filter_1_preEvictionFilter", "Filter_0_preEvictionFilter"}, preEvictionFilterInvocationOrder)
	if diff != "" {
		t.Errorf("check for filter invocation order failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}

	diff = cmp.Diff([]string{"FakePlugin_2", "FakePlugin_0", "FakePlugin_1"}, descheduleInvocationOrder)
	if diff != "" {
		t.Errorf("check for deschedule invocation order failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}

	prfl.RunBalancePlugins(ctx, nodes)

	diff = cmp.Diff([]string{"FakePlugin_1", "FakePlugin_0", "FakePlugin_2"}, balanceInvocationOrder)
	if diff != "" {
		t.Errorf("check for balance invocation order failed. Results are not deep equal. mismatch (-want +got):\n%s", diff)
	}
}
