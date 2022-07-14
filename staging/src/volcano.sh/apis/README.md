## Introduction
volcano-sh/api aims to provide available CRD informers/listers/clientsets generated with different Kubernetes versions for
users. All branches has been named as the Kubernetes version depended on with the format "release-{version}". You can get
CRD informers/listers/clientset under volcano-sh/apis/pkg/client.
## Getting Started
1. Clone the repository to local.
```shell
git clone https://github.com/volcano-sh/apis.git
```
2. Get the CRD informers/listers/clientset under volcano-sh/apis/pkg/client.

## generate

```shell
bash ./hack/generate-groups.sh all volcano.sh/apis/pkg/client volcano.sh/apis/pkg/apis "batch:v1alpha1 bus:v1alpha1 nodeinfo:v1alpha1 scheduling:v1beta1" --go-header-file ./hack/boilerplate.go.txt
```
