package api

import (
	"errors"
	"fmt"
	"strings"
)

var (
	IllegalUtilizationIntervalSecondsMsg = "utilizationIntervalSeconds must be positive"
	IllegalDetectIntervalSecondsMsg      = "detectIntervalSeconds must be positive"
	IllegalHighUsageCountLimitMsg        = "highUsageCountLimit must be positive"

	IllegalQoSCheckIntervalMsg                = "qosCheckInterval must be a positive number"
	IllegalOnlineBandwidthWatermarkPercentMsg = "onlineBandwidthWatermarkPercent must be a positive number between 1 and 1000"
	IllegalOfflineHighBandwidthPercentMsg     = "offlineHighBandwidthPercent must be a positive number between 1 and 1000"
	IllegalOfflineLowBandwidthPercentMsg      = "offlineLowBandwidthPercent must be a positive number between 1 and 1000"
	OfflineHighBandwidthPercentLessLowMsg     = "offlineHighBandwidthPercent cannot be less than offlineLowBandwidthPercent"
	IllegalEvictingCPUHighWatermark           = "evictingCPUHighWatermark must be a positive number"
	IllegalEvictingMemoryHighWatermark        = "evictingMemoryHighWatermark must be a positive number"
	IllegalEvictingCPULowWatermark            = "evictingCPULowWatermark must be a positive number"
	IllegalEvictingMemoryLowWatermark         = "evictingMemoryLowWatermark must be a positive number"
	EvictingCPULowHigherThanHigh              = "cpu evicting low watermark is higher than high watermark"
	EvictingMemoryLowHigherThanHigh           = "memory evicting low watermark is higher than high watermark"
	IllegalOverSubscriptionTypes              = "overSubscriptionType(%s) is not supported, only supports cpu/memory"
)

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
	if n.OnlineBandwidthWatermarkPercent != nil &&
		(*n.OnlineBandwidthWatermarkPercent <= 0 || *n.OnlineBandwidthWatermarkPercent > 1000) {
		errs = append(errs, errors.New(IllegalOnlineBandwidthWatermarkPercentMsg))
	}
	if n.OfflineHighBandwidthPercent != nil &&
		(*n.OfflineHighBandwidthPercent <= 0 || *n.OfflineHighBandwidthPercent > 1000) {
		errs = append(errs, errors.New(IllegalOfflineHighBandwidthPercentMsg))
	}
	if n.OfflineLowBandwidthPercent != nil &&
		(*n.OfflineLowBandwidthPercent <= 0 || *n.OfflineLowBandwidthPercent > 1000) {
		errs = append(errs, errors.New(IllegalOfflineLowBandwidthPercentMsg))
	}
	if n.OfflineLowBandwidthPercent != nil && n.OfflineHighBandwidthPercent != nil &&
		(*n.OfflineLowBandwidthPercent > *n.OfflineHighBandwidthPercent) {
		errs = append(errs, errors.New(OfflineHighBandwidthPercentLessLowMsg))
	}
	return errs
}

func (o *OverSubscription) Validate() []error {
	if o == nil || o.OverSubscriptionTypes == nil {
		return nil
	}

	var errs []error
	for _, t := range strings.Split(*o.OverSubscriptionTypes, ",") {
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
	if e.EvictingCPULowWatermark != nil && e.EvictingCPUHighWatermark != nil &&
		(*e.EvictingCPULowWatermark > *e.EvictingCPUHighWatermark) {
		errs = append(errs, errors.New(EvictingCPULowHigherThanHigh))
	}
	if e.EvictingMemoryLowWatermark != nil && e.EvictingMemoryHighWatermark != nil &&
		(*e.EvictingMemoryLowWatermark > *e.EvictingMemoryHighWatermark) {
		errs = append(errs, errors.New(EvictingMemoryLowHigherThanHigh))
	}
	return errs
}

func (n *NodeMonitor) Validate() []error {
	if n == nil {
		return nil
	}

	var errs []error
	if n.UtilizationIntervalSeconds != nil && *n.UtilizationIntervalSeconds <= 0 {
		errs = append(errs, errors.New(IllegalUtilizationIntervalSecondsMsg))
	}
	if n.DetectIntervalSeconds != nil && *n.DetectIntervalSeconds <= 0 {
		errs = append(errs, errors.New(IllegalDetectIntervalSecondsMsg))
	}
	if n.HighUsageCountLimit != nil && *n.HighUsageCountLimit <= 0 {
		errs = append(errs, errors.New(IllegalHighUsageCountLimitMsg))
	}
	return errs
}

func (c *ColocationConfig) Validate() []error {
	if c == nil {
		return nil
	}

	var errs []error

	if c.NodeLabelConfig != nil {
		errs = append(errs, c.NodeLabelConfig.Validate()...)
	}

	if c.CPUQosConfig != nil {
		errs = append(errs, c.CPUQosConfig.Validate()...)
	}
	if c.CPUBurstConfig != nil {
		errs = append(errs, c.CPUBurstConfig.Validate()...)
	}
	if c.MemoryQosConfig != nil {
		errs = append(errs, c.MemoryQosConfig.Validate()...)
	}
	if c.NetworkQosConfig != nil {
		errs = append(errs, c.NetworkQosConfig.Validate()...)
	}
	if c.OverSubscriptionConfig != nil {
		errs = append(errs, c.OverSubscriptionConfig.Validate()...)
	}
	if c.EvictingConfig != nil {
		errs = append(errs, c.EvictingConfig.Validate()...)
	}
	if c.NodeMonitor != nil {
		errs = append(errs, c.NodeMonitor.Validate()...)
	}

	return errs
}
