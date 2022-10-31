## Introduction
When adapting to K8S V1.25, Volcano introduces the volumebinding package from K8S for self-maintenance. It is expected that the current package will be deleted from Volcano when adapting to K8S V1.29.

## Background
Volcano encounters forward compatibility issues when adapting to K8S V1.25. Volcano can run properly in K8S V1.25, but cannot run in earlier K8S versions (for example, v1.23, v1.21). The error information is `reflector.go:424] k8s.io/client-go/informers/factory.go:134: failed to list *v1.CSIStorageCapacity: the server could not find the requested resource`.

## Reason
The `volumeBinder.csiStorageCapacityLister` in the `k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding` package uses the `k8s.io/client-go/listers/storage/v1beta1` interface in V1.23, and is updated to the `k8s.io/client-go/listers/storage/v1` interface in V1.25. (csiStorageCapacity is officially released v1 in v1.24) In addition, the EnableCSIStorageCapacity feature switch is removed.

The volumeBinder structure is statically defined. If the external plug-in imports the volumebinding package and uses the volumeBinder structure, forward compatibility issues may occur, for example, `v1.CSIStorageCapacity` is not defined.

Volcano references the volumebinding package of the K8S during scheduling for PVC binding. Therefore, after the K8S V1.25 is upgraded, the V1.23 and V1.21 versions cannot run properly.

## Current solution
The `k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding` package is introduced to Volcano for self-maintenance. The `volumeBinder.csiStorageCapacityLister` version is changed back to `k8s.io/client-go/listers/storage/v1beta1` to implement compatibility processing of K8S v1.25, v1.23, and v1.21.

## Future evolution
Solution 1: After the Volcano adapts to K8S V1.27, the `volumeBinder.csiStorageCapacityLister` interface version is upgraded from V1beta1 to V1, the self-maintained volumebinding package is deleted, and the volumebinding package in K8S is referenced again. After the upgrade, however, Only K8S V1.27 and V1.25 are compatible. Earlier versions, such as V1.23, are not compatible.

Solution 2: Add an abstract layer to volumeBinder in the volumebinding package and change the static version dependency to dynamic definition to ensure compatibility between v1beta1 and v1 interfaces.
