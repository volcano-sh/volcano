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

package options

import (
	"github.com/spf13/cobra"

	"volcano.sh/volcano/pkg/networkqos/utils"
)

type Options struct {
	CheckoutInterval         string
	OnlineBandwidthWatermark string
	OfflineLowBandwidth      string
	OfflineHighBandwidth     string
	EnableNetworkQoS         bool
}

// AddFlags is responsible for add flags from the given FlagSet instance for current GenericOptions.
func (o *Options) AddFlags(c *cobra.Command) {
	c.Flags().StringVar(&o.CheckoutInterval, utils.NetWorkQoSCheckInterval, o.CheckoutInterval, "check interval is the interval of checking and updating the offline jobs bandwidth limit")
	c.Flags().StringVar(&o.OnlineBandwidthWatermark, utils.OnlineBandwidthWatermarkKey, o.OnlineBandwidthWatermark, "online-bandwidth-watermark is is the bandwidth threshold of online jobs, "+
		"is the sum of bandwidth of all online pods")
	c.Flags().StringVar(&o.OfflineLowBandwidth, utils.OfflineLowBandwidthKey, o.OfflineLowBandwidth, "offline-low-bandwidth is the maximum amount of network bandwidth that can be used by offline jobs when the"+
		"bandwidth usage of online jobs exceeds the defined threshold(online-bandwidth-watermark)")
	c.Flags().StringVar(&o.OfflineHighBandwidth, utils.OfflineHighBandwidthKey, o.OfflineHighBandwidth, "offline-high-bandwidth is the maximum amount of network bandwidth that can be used by offline jobs when the"+
		"bandwidth usage of online jobs not reach to the defined threshold(online-bandwidth-watermark)")
	c.Flags().BoolVar(&o.EnableNetworkQoS, utils.EnableNetworkQoS, o.EnableNetworkQoS, "enable networkqos")
}
