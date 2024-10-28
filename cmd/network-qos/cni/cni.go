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

package cni

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	cilliumbpf "github.com/cilium/ebpf"
	"github.com/containernetworking/cni/pkg/skel"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnitypesver "github.com/containernetworking/cni/pkg/types/100"
	cniversion "github.com/containernetworking/cni/pkg/version"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/networkqos/api"
	"volcano.sh/volcano/pkg/networkqos/tc"
	"volcano.sh/volcano/pkg/networkqos/throttling"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

func Execute() {
	err := utils.InitLog(utils.CNILogFilePath)
	if err != nil {
		fmt.Printf("failed fo init log: %v\n", err)
	}

	err = PluginMain()
	if err != nil {
		klog.Flush()
		os.Exit(1)
	}

	klog.Flush()
	os.Exit(0)
}

func PluginMain() error {
	e := skel.PluginMainWithError(
		func(args *skel.CmdArgs) error {
			addErr := cmdAdd(args)
			if addErr != nil {
				return fmt.Errorf("CNI add request failed, ContainerID(%s) Netns(%s) IfName(%s) err: %v", args.ContainerID, args.Netns, args.IfName, addErr)
			}
			return nil
		},
		func(args *skel.CmdArgs) error {
			return nil
		},
		func(args *skel.CmdArgs) error {
			delErr := cmdDel(args)
			if delErr != nil {
				return fmt.Errorf("CNI delete request failed, ContainerID(%s) Netns(%s) IfName(%s) err: %v", args.ContainerID, args.Netns, args.IfName, delErr)
			}
			return nil
		},
		cniversion.All, "CNI network-qos plugin")

	if e != nil {
		klog.ErrorS(e, "Failed CNI request")
		if err := e.Print(); err != nil {
			klog.ErrorS(err, "Error writing error JSON to stdout: ", err)
		}
		return e
	}

	return nil
}

func cmdAdd(args *skel.CmdArgs) (err error) {
	klog.InfoS("CNI add request received", "containerID", args.ContainerID,
		"netns", args.Netns, "ifName", args.IfName, "args", args.Args, "path", args.Path, "stdinData", args.StdinData)

	k8sArgs := &api.K8sArgs{}
	err = cnitypes.LoadArgs(args.Args, k8sArgs)
	if err != nil {
		return fmt.Errorf("failed to load k8s config from args: %v", err)
	}

	confRequest := &api.NetConf{}
	err = json.Unmarshal(args.StdinData, confRequest)
	if err != nil {
		return fmt.Errorf("failed to load net config from stdin data: %v", err)
	}

	if err := cniversion.ParsePrevResult(&confRequest.NetConf); err != nil {
		return fmt.Errorf("could not parse prevResult: %v", err)
	}

	if confRequest.PrevResult == nil {
		return fmt.Errorf("must be called as chained plugin")
	}

	prevResult, err := cnitypesver.NewResultFromResult(confRequest.PrevResult)
	if err != nil {
		return fmt.Errorf("failed to convert prevResult: %v", err)
	}
	result := prevResult

	addErr := add(confRequest, args)
	if addErr != nil {
		return fmt.Errorf("failed to add cni: %v", addErr)
	}

	err = cnitypes.PrintResult(result, confRequest.CNIVersion)
	if err != nil {
		return err
	}

	klog.InfoS("CNI add request successfully", "containerID", args.ContainerID,
		"netns", args.Netns, "ifName", args.IfName, "args", args.Args, "path", args.Path, "stdinData", args.StdinData)
	return nil
}

func add(confRequest *api.NetConf, args *skel.CmdArgs) error {
	_, err := throttling.GetNetworkThrottlingConfig().GetThrottlingConfig()
	if err != nil {
		// restart the node ebpf map will be lost
		if errors.Is(err, cilliumbpf.ErrKeyNotExist) || os.IsNotExist(err) {
			if confRequest.Args[utils.NodeColocationEnable] == "true" {
				_, err = throttling.GetNetworkThrottlingConfig().CreateThrottlingConfig(confRequest.Args[utils.OnlineBandwidthWatermarkKey], confRequest.Args[utils.OfflineLowBandwidthKey],
					confRequest.Args[utils.OfflineHighBandwidthKey], confRequest.Args[utils.NetWorkQoSCheckInterval])
				if err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("failed to get throttling config: %v", err)
		}
	}

	support, err := tc.GetTCCmd().PreAddFilter(args.Netns, args.IfName)
	if err != nil {
		return fmt.Errorf("failed to set tc: %v", err)
	}
	if support {
		err = tc.GetTCCmd().AddFilter(args.Netns, args.IfName)
		if err != nil {
			return fmt.Errorf("failed to set tc: %v", err)
		}
	}
	return nil
}

func cmdDel(args *skel.CmdArgs) error {
	klog.InfoS("CNI delete request received", "containerID", args.ContainerID,
		"netns", args.Netns, "ifName", args.IfName, "args", args.Args, "path", args.Path, "stdinData", args.StdinData)

	k8sArgs := &api.K8sArgs{}
	err := cnitypes.LoadArgs(args.Args, k8sArgs)
	if err != nil {
		return fmt.Errorf("failed to load k8s config from args: %v", err)
	}

	err = tc.GetTCCmd().RemoveFilter(args.Netns, args.IfName)
	if err != nil {
		return fmt.Errorf("failed to delete tc: %v", err)
	}

	klog.InfoS("CNI delete request successfully", "containerID", args.ContainerID,
		"netns", args.Netns, "ifName", args.IfName, "args", args.Args, "path", args.Path, "stdinData", args.StdinData)
	return nil
}
