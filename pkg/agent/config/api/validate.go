/*
Copyright 2024 The Volcano Authors.

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
	"errors"
	"fmt"
	"strings"
)

var (
	IllegalQoSCheckIntervalMsg                                   = "qosCheckInterval must be a positive number"
	IllegalOnlineBandwidthWatermarkPercentMsg                    = "onlineBandwidthWatermarkPercent must be a positive number between 1 and 1000"
	IllegalOfflineHighBandwidthPercentMsg                        = "offlineHighBandwidthPercent must be a positive number between 1 and 1000"
	IllegalOfflineLowBandwidthPercentMsg                         = "offlineLowBandwidthPercent must be a positive number between 1 and 1000"
	OfflineHighBandwidthPercentLessOfflineLowBandwidthPercentMsg = "offlineHighBandwidthPercent cannot be less than offlineLowBandwidthPercent"
	IllegalEvictingCPUHighWatermark                              = "evictingCPUHighWatermark must be a positive number"
	IllegalEvictingMemoryHighWatermark                           = "evictingMemoryHighWatermark must be a positive number"
	IllegalEvictingCPULowWatermark                               = "evictingCPULowWatermark must be a positive number"
	IllegalEvictingMemoryLowWatermark                            = "evictingMemoryLowWatermark must be a positive number"
	EvictingCPULowWatermarkHigherThanHighWatermark               = "cpu evicting low watermark is higher than high watermark"
	EvictingMemoryLowWatermarkHigherThanHighWatermark            = "memory evicting low watermark is higher than high watermark"
	IllegalOverSubscriptionTypes                                 = "overSubscriptionType(%s) is not supported, only supports cpu/memory"
)

type Validate interface {
	Validate() []error
}

func (n *NodeLabelConfig) Validate() []error {
	return nil
}

func (c *CPUQos) Validate() []error {
	return nil
}

func (c *CPUBurst) Validate() []error {
	return nil
}

func (m *MemoryQos) Validate() []error {
	return nil
}

func (n *NetworkQos) Validate() []error {
	if n == nil {
		return nil
	}

	var errs []error
	if n.QoSCheckInterval != nil && *n.QoSCheckInterval <= 0 {
		errs = append(errs, errors.New(IllegalQoSCheckIntervalMsg))
	}

	// Maximum Bandwidth may be several times the Baseline Bandwidth.
	// Use 1000 ( 10 times the Baseline Bandwidth ) as the upper limit.
	if n.OnlineBandwidthWatermarkPercent != nil && (*n.OnlineBandwidthWatermarkPercent <= 0 || *n.OnlineBandwidthWatermarkPercent > 1000) {
		errs = append(errs, errors.New(IllegalOnlineBandwidthWatermarkPercentMsg))
	}
	if n.OfflineHighBandwidthPercent != nil && (*n.OfflineHighBandwidthPercent <= 0 || *n.OfflineHighBandwidthPercent > 1000) {
		errs = append(errs, errors.New(IllegalOfflineHighBandwidthPercentMsg))
	}
	if n.OfflineLowBandwidthPercent != nil && (*n.OfflineLowBandwidthPercent <= 0 || *n.OfflineLowBandwidthPercent > 1000) {
		errs = append(errs, errors.New(IllegalOfflineLowBandwidthPercentMsg))
	}
	if n.OfflineLowBandwidthPercent != nil && n.OfflineHighBandwidthPercent != nil && (*n.OfflineLowBandwidthPercent > *n.OfflineHighBandwidthPercent) {
		errs = append(errs, errors.New(OfflineHighBandwidthPercentLessOfflineLowBandwidthPercentMsg))
	}
	return errs
}

func (o *OverSubscription) Validate() []error {
	if o == nil {
		return nil
	}
	if o.OverSubscriptionTypes == nil || len(*o.OverSubscriptionTypes) == 0 {
		return nil
	}

	var errs []error
	types := strings.Split(*o.OverSubscriptionTypes, ",")
	for _, t := range types {
		if t != "cpu" && t != "memory" {
			errs = append(errs, fmt.Errorf(IllegalOverSubscriptionTypes, t))
		}
	}
	return errs
}

func (e *Evicting) Validate() []error {
	if e == nil {
		return nil
	}

	var errs []error
	if e.EvictingCPUHighWatermark != nil && *e.EvictingCPUHighWatermark <= 0 {
		errs = append(errs, errors.New(IllegalEvictingCPUHighWatermark))
	}
	if e.EvictingMemoryHighWatermark != nil && *e.EvictingMemoryHighWatermark <= 0 {
		errs = append(errs, errors.New(IllegalEvictingMemoryHighWatermark))
	}
	if e.EvictingCPULowWatermark != nil && *e.EvictingCPULowWatermark <= 0 {
		errs = append(errs, errors.New(IllegalEvictingCPULowWatermark))
	}
	if e.EvictingMemoryLowWatermark != nil && *e.EvictingMemoryLowWatermark <= 0 {
		errs = append(errs, errors.New(IllegalEvictingMemoryLowWatermark))
	}
	if e.EvictingCPULowWatermark != nil && e.EvictingCPUHighWatermark != nil && (*e.EvictingCPULowWatermark > *e.EvictingCPUHighWatermark) {
		errs = append(errs, errors.New(EvictingCPULowWatermarkHigherThanHighWatermark))
	}
	if e.EvictingMemoryLowWatermark != nil && e.EvictingMemoryHighWatermark != nil && (*e.EvictingMemoryLowWatermark > *e.EvictingMemoryHighWatermark) {
		errs = append(errs, errors.New(EvictingMemoryLowWatermarkHigherThanHighWatermark))
	}
	return errs
}

func (c *ColocationConfig) Validate() []error {
	if c == nil {
		return nil
	}
	var errs []error

	errs = append(errs, c.NodeLabelConfig.Validate()...)
	errs = append(errs, c.CPUQosConfig.Validate()...)
	errs = append(errs, c.CPUBurstConfig.Validate()...)
	errs = append(errs, c.MemoryQosConfig.Validate()...)
	errs = append(errs, c.NetworkQosConfig.Validate()...)
	errs = append(errs, c.OverSubscriptionConfig.Validate()...)
	errs = append(errs, c.EvictingConfig.Validate()...)
	return errs
}
