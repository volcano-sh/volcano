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
	"path/filepath"
	"strings"

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
			return fmt.Errorf("failed to list clsact qdiscs on ns:ifName(%s:%s): %v", netns, ifName, err)
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

		existing, err := listExistingQdiscs(link)
		if err != nil {
			return fmt.Errorf("failed to list qdiscs on ns:ifName(%s:%s): %v", netns, ifName, err)
		}

		// PreAddFilter accepts an interface that already has a root fq qdisc
		// or a clsact qdisc, so reuse them here instead of failing with EEXIST.
		// Only the qdiscs created by this call are rolled back on failure, so a
		// partial failure never leaves state behind that breaks the next ADD,
		// and qdiscs installed by someone else are never removed.
		var created []netlink.Qdisc
		success := false
		defer func() {
			if success {
				return
			}
			for i := len(created) - 1; i >= 0; i-- {
				if delErr := netlink.QdiscDel(created[i]); delErr != nil {
					klog.ErrorS(delErr, "Failed to roll back qdisc", "type", created[i].Type(), "netns", netns, "ifName", ifName)
				}
			}
		}()

		if existing.rootFq {
			klog.InfoS("Root fq qdisc already exists, reusing it", "netns", netns, "ifName", ifName)
		} else {
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
			created = append(created, fq)
			klog.InfoS("Successfully added qdisc", "type", QdiscTypeFq, "netns", netns, "ifName", ifName)
		}

		if existing.clsact {
			klog.InfoS("Clsact qdisc already exists, reusing it", "netns", netns, "ifName", ifName)
		} else {
			clsact := &netlink.GenericQdisc{
				QdiscAttrs: netlink.QdiscAttrs{
					LinkIndex: link.Attrs().Index,
					Parent:    netlink.HANDLE_CLSACT,
				},
				QdiscType: QdiscTypeClsact,
			}
			if err = netlink.QdiscAdd(clsact); err != nil {
				return fmt.Errorf("failed to create clsact qdisc on ns:ifName(%s:%s): %v", netns, ifName, err)
			}
			created = append(created, clsact)
			klog.InfoS("Successfully added qdisc", "type", QdiscTypeClsact, "netns", netns, "ifName", ifName)
		}

		// Do not attach the program a second time if a previous ADD already did.
		if existing.clsact {
			attached, err := hasNetworkQoSFilter(link)
			if err != nil {
				return fmt.Errorf("failed to list egress filters on ns:ifName(%s:%s): %v", netns, ifName, err)
			}
			if attached {
				klog.InfoS("Network qos egress filter already attached", "ebpf-obj", utils.TCPROGPath, "netns", netns, "ifName", ifName)
				success = true
				return nil
			}
		}

		filterCmd := fmt.Sprintf("tc filter add dev %s egress bpf direct-action obj %s sec tc", ifName, utils.TCPROGPath)
		ctx, cancel := context.WithTimeout(context.Background(), CmdTimeout)
		defer cancel()
		output, err := exec.GetExecutor().CommandContext(ctx, filterCmd)
		if err != nil {
			return fmt.Errorf("add dev on ns:ifName(%s:%s) failed : %v, %s, %s", netns, ifName, err, output, filterCmd)
		}
		klog.InfoS("Successfully added filter", "ebpf-obj", utils.TCPROGPath, "netns", netns, "ifName", ifName)
		success = true
		return nil
	})

	return addErr
}

// existingQdiscs records which of the qdiscs needed by network qos are
// already installed on a link.
type existingQdiscs struct {
	rootFq bool
	clsact bool
}

func listExistingQdiscs(link netlink.Link) (existingQdiscs, error) {
	var result existingQdiscs
	qdiscs, err := netlink.QdiscList(link)
	if err != nil {
		return result, err
	}
	for _, qdisc := range qdiscs {
		switch {
		case qdisc.Attrs().Parent == netlink.HANDLE_ROOT && qdisc.Type() == QdiscTypeFq:
			result.rootFq = true
		case qdisc.Attrs().Parent == netlink.HANDLE_CLSACT && qdisc.Type() == QdiscTypeClsact:
			result.clsact = true
		}
	}
	return result, nil
}

// hasNetworkQoSFilter reports whether the network qos bpf program is already
// attached to the egress hook of link. tc names a bpf filter loaded from an
// object file "<file name>:[<section>]".
func hasNetworkQoSFilter(link netlink.Link) (bool, error) {
	filters, err := netlink.FilterList(link, netlink.HANDLE_MIN_EGRESS)
	if err != nil {
		return false, err
	}
	prefix := filepath.Base(utils.TCPROGPath) + ":"
	for _, f := range filters {
		if bpf, ok := f.(*netlink.BpfFilter); ok && strings.HasPrefix(bpf.Name, prefix) {
			return true, nil
		}
	}
	return false, nil
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
