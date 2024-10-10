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

type EbpfNetThrottlingConfGetResult struct {
	WaterLine string `json:"online_bandwidth_watermark"`
	Interval  uint64 `json:"interval"`
	LowRate   string `json:"offline_low_bandwidth"`
	HighRate  string `json:"offline_high_bandwidth"`
}

type EbpfNetThrottlingConfig struct {
	// WaterLine is the bandwidth threshold of online jobs, is the sum of bandwidth of all online pods
	// If the bandwidth usage of the online jobs exceeds this value,
	// the total network bandwidth usage of offline jobs will not exceed the value defined by LowRate.
	// Otherwise, the total bandwidth usage of offline jobs will not be allowed to exceed the value defined by HighRate.
	WaterLine uint64 `json:"online_bandwidth_watermark"`
	// Interval is the check period of statistical network bandwidth usage
	Interval uint64 `json:"interval"`
	// LowRate is the maximum amount of network bandwidth that can be used by offline jobs when the
	// bandwidth usage of online jobs exceeds the defined threshold(waterline)
	LowRate uint64 `json:"offline_low_bandwidth"`
	// HighRate is the maximum amount of network bandwidth that can be used by offline jobs when the
	// bandwidth usage of online jobs not reach to the defined threshold(waterline)
	HighRate uint64 `json:"offline_high_bandwidth"`
}

type EbpfNetThrottlingStatus struct {
	// CheckTimes is the total number of times the checking occurred
	CheckTimes uint64 `json:"check_times"`
	// HighTimes is the total number of checking times that the bandwidth usage of online jobs exceeds the threshold
	HighTimes uint64 `json:"high_times"`
	// LowTimes is the total number of checking times that the bandwidth usage of online jobs not reach to the threshold
	LowTimes uint64 `json:"low_times"`
	// OnlinePKTs is the total number of bytes in the network packet for online jobs
	OnlinePKTs uint64 `json:"online_tx_packages"`
	// OfflinePKTs is the total number of bytes in the network packet for offline jobs
	OfflinePKTs uint64 `json:"offline_tx_packages"`
	OfflinePrio uint64 `json:"offline_prio"`
	// RatePast is the network bandwidth of offline jobs in the latest check
	RatePast uint64 `json:"latest_online_bandwidth"`
	// OfflineRatePast is the network bandwidth of online jobs in the latest check
	OfflineRatePast uint64 `json:"latest_offline_bandwidth"`
}

type EbpfNetThrottling struct {
	// TLast is the latest time an offline job's network packet is issued
	TLast uint64 `json:"latest_offline_packet_send_time"`
	// Rate is the maximum amount of network bandwidth that can be used by offline jobs
	Rate uint64 `json:"offline_bandwidth_limit"`
	// TXBytes is the total number of bytes of network packets of offline jobs
	TXBytes uint64 `json:"offline_tx_bytes"`
	// OnlineTXBytes is the total number of bytes of network packets of online jobs
	OnlineTXBytes uint64 `json:"online_tx_bytes"`
	// TStart is the last check time
	TStart uint64 `json:"latest_check_time"`
	EbpfNetThrottlingStatus
}
