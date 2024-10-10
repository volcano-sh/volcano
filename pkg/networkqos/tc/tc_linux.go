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

package tc

import (
	"context"
	"fmt"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/utils/exec"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

const (
	QdiscTypeClsact  = "clsact"
	QdiscTypeFq      = "fq"
	FilterTypeBpf    = "bpf"
	QdiscTypeNoQueue = "noqueue"
)

func (t *TCCmd) PreAddFilter(netns, ifName string) (bool, error) {
	netNs, err := ns.GetNS(netns)
	if err != nil {
		err = fmt.Errorf("failed to open netns %s: %v", netns, err)
		return false, err
	}
	defer netNs.Close()

	supportAddFilter := true
	checkErr := netNs.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to lookup device on ns:ifName(%s:%s): %v", netns, ifName, err)
		}

		qdiscs, err := netlink.QdiscList(link)
		if err != nil {
			return fmt.Errorf("failed to list clsact qdiscs on on ns:ifName(%s:%s): %v", netns, ifName, err)
		}

		for _, qdisc := range qdiscs {
			if qdisc.Attrs().Parent == netlink.HANDLE_INGRESS && qdisc.Type() != QdiscTypeClsact {
				klog.InfoS("the Ingress qdisc already existed, can not create new one, skip add network qos qdisc to this device",
					"namespace", netns, "ifName", ifName, "existed-ingress-qdisc", qdisc.Type())
				supportAddFilter = false
				break
			}

			if qdisc.Attrs().Parent == netlink.HANDLE_ROOT && qdisc.Type() != QdiscTypeFq && qdisc.Type() != QdiscTypeNoQueue {
				klog.InfoS("the root handle qdisc already existed, can not create new one, skip add network qos qdisc to this device",
					"namespace", netns, "ifName", ifName, "existed-root-qdisc", qdisc.Type())
				supportAddFilter = false
				break
			}
		}
		return nil
	})
	return supportAddFilter, checkErr
}

func (t *TCCmd) AddFilter(netns, ifName string) error {
	netNs, err := ns.GetNS(netns)
	if err != nil {
		err = fmt.Errorf("failed to open netns %s: %v", netns, err)
		return err
	}
	defer netNs.Close()

	addErr := netNs.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to lookup device on ns:ifName(%s:%s): %v", netns, ifName, err)
		}

		fq := &netlink.Fq{
			QdiscAttrs: netlink.QdiscAttrs{
				LinkIndex: link.Attrs().Index,
				Parent:    netlink.HANDLE_ROOT,
			},
			Pacing: 1,
		}
		if err = netlink.QdiscAdd(fq); err != nil {
			return fmt.Errorf("failed to create fq qdisc ns:ifName(%s:%s): %v", netns, ifName, err)
		}
		klog.InfoS("Successfully added qdisc", "type", QdiscTypeFq, "netns", netns, "ifName", ifName)

		clsact := &netlink.GenericQdisc{
			QdiscAttrs: netlink.QdiscAttrs{
				LinkIndex: link.Attrs().Index,
				Parent:    netlink.HANDLE_CLSACT,
			},
			QdiscType: QdiscTypeClsact,
		}
		if err = netlink.QdiscAdd(clsact); err != nil {
			return fmt.Errorf("failed to create clsact qdisc on on ns:ifName(%s:%s): %v", netns, ifName, err)
		}
		klog.InfoS("Successfully added qdisc", "type", QdiscTypeClsact, "netns", netns, "ifName", ifName)

		filterCmd := fmt.Sprintf("tc filter add dev %s egress bpf direct-action obj %s sec tc", ifName, utils.TCPROGPath)
		ctx, cancel := context.WithTimeout(context.Background(), CmdTimeout)
		defer cancel()
		output, err := exec.GetExecutor().CommandContext(ctx, filterCmd)
		if err != nil {
			return fmt.Errorf("add dev on ns:ifName(%s:%s) failed : %v, %s, %s", netns, ifName, err, output, filterCmd)
		}
		klog.InfoS("Successfully added filter", "ebpf-obj", utils.TCPROGPath, "netns", netns, "ifName", ifName)
		return nil
	})

	return addErr
}

func (t *TCCmd) RemoveFilter(netns, ifName string) error {
	netNs, err := ns.GetNS(netns)
	if err != nil {
		_, ok := err.(ns.NSPathNotExistErr)
		if ok {
			return nil
		}
		err = fmt.Errorf("failed to open netns %s: %v", netns, err)
		return err
	}
	defer netNs.Close()

	removeErr := netNs.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			_, ok := err.(netlink.LinkNotFoundError)
			if ok {
				return nil
			}
			return fmt.Errorf("failed to lookup device on ns:ifName(%s:%s): %v", netns, ifName, err)
		}

		filters, err := netlink.FilterList(link, netlink.HANDLE_MIN_EGRESS)
		if err != nil {
			return fmt.Errorf("failed to list engress filters on ns:ifName(%s:%s): %v", netns, ifName, err)
		}

		for _, f := range filters {
			if f.Type() == FilterTypeBpf {
				if err = netlink.FilterDel(f); err != nil {
					return fmt.Errorf("failed to delete egress filters on ns:ifName(%s:%s): %v", netns, ifName, err)
				}
				klog.InfoS("Successfully deleted egress filter", "netns", netns, "ifName", ifName)
				break
			}
		}

		qdiscs, err := netlink.QdiscList(link)
		if err != nil {
			return fmt.Errorf("failed to list qdiscs %s: %v", ifName, err)
		}

		for _, qdisc := range qdiscs {
			if qdisc.Type() == QdiscTypeClsact || qdisc.Type() == QdiscTypeFq {
				if err = netlink.QdiscDel(qdisc); err != nil {
					return fmt.Errorf("failed to delete %s qdisc on ns:ifName(%s:%s): %v", qdisc.Type(), netns, ifName, err)
				}
				klog.InfoS("Successfully deleted qdisc", "type", qdisc.Type(), "netns", netns, "ifName", ifName)
			}
		}
		return nil
	})

	return removeErr
}
