/*
Copyright 2023 The Kubernetes Authors.
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
- [2024]Abstract evictor as an interface
*/

package profile

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/descheduler/metrics"
	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/descheduler/evictions"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	"sigs.k8s.io/descheduler/pkg/framework/pluginregistry"
	frameworktypes "sigs.k8s.io/descheduler/pkg/framework/types"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"

	"k8s.io/klog/v2"
)

const DefaultEvictor string = "default"

// evictorImpl implements the Evictor interface so plugins
// can evict a pod without importing a specific pod evictor
type evictorImpl struct {
	podEvictor        *evictions.PodEvictor
	filter            podutil.FilterFunc
	preEvictionFilter podutil.FilterFunc
}

var _ frameworktypes.Evictor = &evictorImpl{}

// Filter checks if a pod can be evicted
func (ei *evictorImpl) Filter(pod *v1.Pod) bool {
	return ei.filter(pod)
}

// PreEvictionFilter checks if pod can be evicted right before eviction
func (ei *evictorImpl) PreEvictionFilter(pod *v1.Pod) bool {
	return ei.preEvictionFilter(pod)
}

// Evict evicts a pod (no pre-check performed)
func (ei *evictorImpl) Evict(ctx context.Context, pod *v1.Pod, opts evictions.EvictOptions) bool {
	return ei.podEvictor.EvictPod(ctx, pod, opts)
}

func (ei *evictorImpl) NodeLimitExceeded(node *v1.Node) bool {
	return ei.podEvictor.NodeLimitExceeded(node)
}

// handleImpl implements the framework handle which gets passed to plugins
type handleImpl struct {
	clientSet                 clientset.Interface
	getPodsAssignedToNodeFunc podutil.GetPodsAssignedToNodeFunc
	sharedInformerFactory     informers.SharedInformerFactory
	evictor                   *evictorImpl
}

var _ frameworktypes.Handle = &handleImpl{}

// ClientSet retrieves kube client set
func (hi *handleImpl) ClientSet() clientset.Interface {
	return hi.clientSet
}

// GetPodsAssignedToNodeFunc retrieves GetPodsAssignedToNodeFunc implementation
func (hi *handleImpl) GetPodsAssignedToNodeFunc() podutil.GetPodsAssignedToNodeFunc {
	return hi.getPodsAssignedToNodeFunc
}

// SharedInformerFactory retrieves shared informer factory
func (hi *handleImpl) SharedInformerFactory() informers.SharedInformerFactory {
	return hi.sharedInformerFactory
}

// Evictor retrieves evictor so plugins can filter and evict pods
func (hi *handleImpl) Evictor() frameworktypes.Evictor {
	return hi.evictor
}

type filterPlugin interface {
	frameworktypes.Plugin
	Filter(pod *v1.Pod) bool
}

type preEvictionFilterPlugin interface {
	frameworktypes.Plugin
	PreEvictionFilter(pod *v1.Pod) bool
}
type Evictor interface {
	NodeEvicted(node *v1.Node) uint
	TotalEvicted() uint
	NodeLimitExceeded(node *v1.Node) bool
	EvictPod(ctx context.Context, pod *v1.Pod, opts evictions.EvictOptions) bool
}
type profileImpl struct {
	profileName string
	podEvictor  Evictor

	deschedulePlugins        []frameworktypes.DeschedulePlugin
	balancePlugins           []frameworktypes.BalancePlugin
	filterPlugins            []filterPlugin
	preEvictionFilterPlugins []preEvictionFilterPlugin

	// Each extension point with a list of plugins implementing the extension point.
	deschedule        sets.String
	balance           sets.String
	filter            sets.String
	preEvictionFilter sets.String
}

// Option for the handleImpl.
type Option func(*handleImplOpts)

type handleImplOpts struct {
	clientSet                 clientset.Interface
	sharedInformerFactory     informers.SharedInformerFactory
	getPodsAssignedToNodeFunc podutil.GetPodsAssignedToNodeFunc
	podEvictor                *evictions.PodEvictor
}

// WithClientSet sets clientSet for the scheduling frameworkImpl.
func WithClientSet(clientSet clientset.Interface) Option {
	return func(o *handleImplOpts) {
		o.clientSet = clientSet
	}
}

func WithSharedInformerFactory(sharedInformerFactory informers.SharedInformerFactory) Option {
	return func(o *handleImplOpts) {
		o.sharedInformerFactory = sharedInformerFactory
	}
}

func WithPodEvictor(podEvictor *evictions.PodEvictor) Option {
	return func(o *handleImplOpts) {
		o.podEvictor = podEvictor
	}
}

func WithGetPodsAssignedToNodeFnc(getPodsAssignedToNodeFunc podutil.GetPodsAssignedToNodeFunc) Option {
	return func(o *handleImplOpts) {
		o.getPodsAssignedToNodeFunc = getPodsAssignedToNodeFunc
	}
}

func getPluginConfig(pluginName string, pluginConfigs []api.PluginConfig) (*api.PluginConfig, int) {
	for idx, pluginConfig := range pluginConfigs {
		if pluginConfig.Name == pluginName {
			return &pluginConfig, idx
		}
	}
	return nil, 0
}

func buildPlugin(config api.DeschedulerProfile, pluginName string, handle *handleImpl, reg pluginregistry.Registry) (frameworktypes.Plugin, error) {
	pc, _ := getPluginConfig(pluginName, config.PluginConfigs)
	if pc == nil {
		klog.ErrorS(fmt.Errorf("unable to get plugin config"), "skipping plugin", "plugin", pluginName, "profile", config.Name)
		return nil, fmt.Errorf("unable to find %q plugin config", pluginName)
	}

	registryPlugin, ok := reg[pluginName]
	if !ok {
		klog.ErrorS(fmt.Errorf("unable to find plugin in the pluginsMap"), "skipping plugin", "plugin", pluginName)
		return nil, fmt.Errorf("unable to find %q plugin in the pluginsMap", pluginName)
	}
	pg, err := registryPlugin.PluginBuilder(pc.Args, handle)
	if err != nil {
		klog.ErrorS(err, "unable to initialize a plugin", "pluginName", pluginName)
		return nil, fmt.Errorf("unable to initialize %q plugin: %v", pluginName, err)
	}
	return pg, nil
}

func (p *profileImpl) registryToExtensionPoints(registry pluginregistry.Registry) {
	p.deschedule = sets.NewString()
	p.balance = sets.NewString()
	p.filter = sets.NewString()
	p.preEvictionFilter = sets.NewString()

	for plugin, pluginUtilities := range registry {
		if _, ok := pluginUtilities.PluginType.(frameworktypes.DeschedulePlugin); ok {
			p.deschedule.Insert(plugin)
		}
		if _, ok := pluginUtilities.PluginType.(frameworktypes.BalancePlugin); ok {
			p.balance.Insert(plugin)
		}
		if _, ok := pluginUtilities.PluginType.(frameworktypes.EvictorPlugin); ok {
			p.filter.Insert(plugin)
			p.preEvictionFilter.Insert(plugin)
		}
	}
}

func NewProfile(config api.DeschedulerProfile, reg pluginregistry.Registry, opts ...Option) (*profileImpl, error) {
	hOpts := &handleImplOpts{}
	for _, optFnc := range opts {
		optFnc(hOpts)
	}

	if hOpts.clientSet == nil {
		return nil, fmt.Errorf("clientSet missing")
	}

	if hOpts.sharedInformerFactory == nil {
		return nil, fmt.Errorf("sharedInformerFactory missing")
	}

	if hOpts.podEvictor == nil {
		return nil, fmt.Errorf("podEvictor missing")
	}
	pi := &profileImpl{
		profileName:              config.Name,
		podEvictor:               hOpts.podEvictor,
		deschedulePlugins:        []frameworktypes.DeschedulePlugin{},
		balancePlugins:           []frameworktypes.BalancePlugin{},
		filterPlugins:            []filterPlugin{},
		preEvictionFilterPlugins: []preEvictionFilterPlugin{},
	}
	pi.registryToExtensionPoints(reg)

	if !pi.deschedule.HasAll(config.Plugins.Deschedule.Enabled...) {
		return nil, fmt.Errorf("profile %q configures deschedule extension point of non-existing plugins: %v", config.Name, sets.NewString(config.Plugins.Deschedule.Enabled...).Difference(pi.deschedule))
	}
	if !pi.balance.HasAll(config.Plugins.Balance.Enabled...) {
		return nil, fmt.Errorf("profile %q configures balance extension point of non-existing plugins: %v", config.Name, sets.NewString(config.Plugins.Balance.Enabled...).Difference(pi.balance))
	}
	if !pi.filter.HasAll(config.Plugins.Filter.Enabled...) {
		return nil, fmt.Errorf("profile %q configures filter extension point of non-existing plugins: %v", config.Name, sets.NewString(config.Plugins.Filter.Enabled...).Difference(pi.filter))
	}
	if !pi.preEvictionFilter.HasAll(config.Plugins.PreEvictionFilter.Enabled...) {
		return nil, fmt.Errorf("profile %q configures preEvictionFilter extension point of non-existing plugins: %v", config.Name, sets.NewString(config.Plugins.PreEvictionFilter.Enabled...).Difference(pi.preEvictionFilter))
	}

	handle := &handleImpl{
		clientSet:                 hOpts.clientSet,
		getPodsAssignedToNodeFunc: hOpts.getPodsAssignedToNodeFunc,
		sharedInformerFactory:     hOpts.sharedInformerFactory,
		evictor: &evictorImpl{
			podEvictor: hOpts.podEvictor,
		},
	}

	pluginNames := append(config.Plugins.Deschedule.Enabled, config.Plugins.Balance.Enabled...)
	pluginNames = append(pluginNames, config.Plugins.Filter.Enabled...)
	pluginNames = append(pluginNames, config.Plugins.PreEvictionFilter.Enabled...)

	plugins := make(map[string]frameworktypes.Plugin)
	for _, plugin := range sets.NewString(pluginNames...).List() {
		pg, err := buildPlugin(config, plugin, handle, reg)
		if err != nil {
			return nil, fmt.Errorf("unable to build %v plugin: %v", plugin, err)
		}
		if pg == nil {
			return nil, fmt.Errorf("got empty %v plugin build", plugin)
		}
		plugins[plugin] = pg
	}

	// Later, when a default list of plugins and their extension points is established,
	// compute the list of enabled extension points as (DefaultEnabled + Enabled - Disabled)
	for _, pluginName := range config.Plugins.Deschedule.Enabled {
		pi.deschedulePlugins = append(pi.deschedulePlugins, plugins[pluginName].(frameworktypes.DeschedulePlugin))
	}

	for _, pluginName := range config.Plugins.Balance.Enabled {
		pi.balancePlugins = append(pi.balancePlugins, plugins[pluginName].(frameworktypes.BalancePlugin))
	}

	filters := []podutil.FilterFunc{}
	for _, pluginName := range config.Plugins.Filter.Enabled {
		pi.filterPlugins = append(pi.filterPlugins, plugins[pluginName].(filterPlugin))
		filters = append(filters, plugins[pluginName].(filterPlugin).Filter)
	}

	preEvictionFilters := []podutil.FilterFunc{}
	for _, pluginName := range config.Plugins.PreEvictionFilter.Enabled {
		pi.preEvictionFilterPlugins = append(pi.preEvictionFilterPlugins, plugins[pluginName].(preEvictionFilterPlugin))
		preEvictionFilters = append(preEvictionFilters, plugins[pluginName].(preEvictionFilterPlugin).PreEvictionFilter)
	}

	handle.evictor.filter = podutil.WrapFilterFuncs(filters...)
	handle.evictor.preEvictionFilter = podutil.WrapFilterFuncs(preEvictionFilters...)

	return pi, nil
}

func (d profileImpl) RunDeschedulePlugins(ctx context.Context, nodes []*v1.Node) *frameworktypes.Status {
	errs := []error{}
	for _, pl := range d.deschedulePlugins {
		evicted := d.podEvictor.TotalEvicted()
		// TODO: strategyName should be accessible from within the strategy using a framework
		// handle or function which the Evictor has access to. For migration/in-progress framework
		// work, we are currently passing this via context. To be removed
		// (See discussion thread https://github.com/kubernetes-sigs/descheduler/pull/885#discussion_r919962292)
		strategyStart := time.Now()
		childCtx := context.WithValue(ctx, "strategyName", pl.Name())
		status := pl.Deschedule(childCtx, nodes)
		metrics.DeschedulerStrategyDuration.With(map[string]string{"strategy": pl.Name(), "profile": d.profileName}).Observe(time.Since(strategyStart).Seconds())

		if status != nil && status.Err != nil {
			errs = append(errs, fmt.Errorf("plugin %q finished with error: %v", pl.Name(), status.Err))
		}
		klog.V(1).InfoS("Total number of pods evicted", "extension point", "Deschedule", "evictedPods", d.podEvictor.TotalEvicted()-evicted)
	}

	aggrErr := errors.NewAggregate(errs)
	if aggrErr == nil {
		return &frameworktypes.Status{}
	}

	return &frameworktypes.Status{
		Err: fmt.Errorf("%v", aggrErr.Error()),
	}
}

func (d profileImpl) RunBalancePlugins(ctx context.Context, nodes []*v1.Node) *frameworktypes.Status {
	errs := []error{}
	for _, pl := range d.balancePlugins {
		evicted := d.podEvictor.TotalEvicted()
		// TODO: strategyName should be accessible from within the strategy using a framework
		// handle or function which the Evictor has access to. For migration/in-progress framework
		// work, we are currently passing this via context. To be removed
		// (See discussion thread https://github.com/kubernetes-sigs/descheduler/pull/885#discussion_r919962292)
		strategyStart := time.Now()
		childCtx := context.WithValue(ctx, "strategyName", pl.Name())
		status := pl.Balance(childCtx, nodes)
		metrics.DeschedulerStrategyDuration.With(map[string]string{"strategy": pl.Name(), "profile": d.profileName}).Observe(time.Since(strategyStart).Seconds())

		if status != nil && status.Err != nil {
			errs = append(errs, fmt.Errorf("plugin %q finished with error: %v", pl.Name(), status.Err))
		}
		klog.V(1).InfoS("Total number of pods evicted", "extension point", "Balance", "evictedPods", d.podEvictor.TotalEvicted()-evicted)
	}

	aggrErr := errors.NewAggregate(errs)
	if aggrErr == nil {
		return &frameworktypes.Status{}
	}

	return &frameworktypes.Status{
		Err: fmt.Errorf("%v", aggrErr.Error()),
	}
}
