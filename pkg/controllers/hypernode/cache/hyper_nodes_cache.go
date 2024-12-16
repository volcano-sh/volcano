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

package cache

import (
	"errors"
	"fmt"
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

var (
	NotFoundError = errors.New("not found")
)

type HyperNodesCache struct {
	// The indexer called HyperNodes is used to store pointers of all hypernodes,
	// including those hypernodes already listed/watched to the controller,
	// and those that have sent hypernode create/update/delete requests to the api-server but have not yet listed/watched
	// to the controller's cache
	HyperNodes cache.Indexer
	// MembersToHyperNode is used to find the hypernode it belongs to based on the member,
	// so that we can quickly find the hypernode and to update its members.
	// The key is member's name, value is the pointer of HyperNode.
	MembersToHyperNode sync.Map
}

func NewHyperNodesCache(store cache.Indexer) *HyperNodesCache {
	return &HyperNodesCache{
		HyperNodes: store,
	}
}

func (h *HyperNodesCache) GetHyperNode(name string) (*topologyv1alpha1.HyperNode, error) {
	obj, ok, err := h.HyperNodes.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: hypernode %s does not exists in local cache", NotFoundError, name)
	}

	hn := obj.(*topologyv1alpha1.HyperNode)
	return hn, nil
}

func (h *HyperNodesCache) UpdateHyperNode(newHN *topologyv1alpha1.HyperNode) error {
	name, err := cache.MetaNamespaceKeyFunc(newHN)
	if err != nil {
		return err
	}

	storedHN, err := h.GetHyperNode(name)
	if err != nil {
		if !errors.Is(err, NotFoundError) {
			return fmt.Errorf("failed to update hypernode %s: %v", newHN.Name, err)
		}
		err = h.HyperNodes.Add(newHN)
		if err != nil {
			return fmt.Errorf("failed to add hypernode %s to local cache: %v", newHN.Name, err)
		}
		return nil
	}

	storedRV, err := h.getResourceVersion(storedHN)
	if err != nil {
		return err
	}

	newRV, err := h.getResourceVersion(newHN)
	if err != nil {
		return err
	}

	if newRV <= storedRV {
		// if the new hypernode's resource version is less or equal to the already stored hypernode's resource version, we do nothing
		return nil
	}

	err = h.HyperNodes.Update(newHN)
	if err != nil {
		return err
	}

	return nil
}

func (h *HyperNodesCache) DeleteHyperNode(hn *topologyv1alpha1.HyperNode) error {
	err := h.HyperNodes.Delete(hn)
	if err != nil {
		return err
	}

	return nil
}

func (h *HyperNodesCache) getResourceVersion(hn *topologyv1alpha1.HyperNode) (int64, error) {
	objAccessor, err := meta.Accessor(hn)
	if err != nil {
		return -1, err
	}

	objResourceVersion, err := strconv.ParseInt(objAccessor.GetResourceVersion(), 10, 64)
	if err != nil {
		return -1, fmt.Errorf("error parsing ResourceVersion %q for %s: %v", objAccessor.GetResourceVersion(), hn.Name, err)
	}
	return objResourceVersion, nil
}
