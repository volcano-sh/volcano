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

package util

import (
	"context"

	. "github.com/onsi/gomega"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateResourceClaim creates a ResourceClaim in the test context's namespace
func CreateResourceClaim(ctx *TestContext, claim *resourcev1.ResourceClaim) *resourcev1.ResourceClaim {
	if claim.Namespace == "" {
		claim.Namespace = ctx.Namespace
	}
	c, err := ctx.Kubeclient.ResourceV1().ResourceClaims(claim.Namespace).Create(context.TODO(), claim, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create ResourceClaim %s in namespace %s", claim.Name, claim.Namespace)
	return c
}
