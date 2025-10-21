/*
Copyright 2025 The Volcano Authors.

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

package admission

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hypernodev1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/test/e2e/util"
)

var _ = ginkgo.Describe("HyperNode Validating E2E Test", func() {

	ginkgo.It("Should allow HyperNode creation with valid exactMatch selector", func() {
		hyperNodeName := "valid-exact-match-hypernode"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							ExactMatch: &hypernodev1alpha1.ExactMatch{
								Name: "node-1",
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Delete(context.TODO(), hyperNodeName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow HyperNode creation with valid regexMatch selector", func() {
		hyperNodeName := "valid-regex-match-hypernode"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							RegexMatch: &hypernodev1alpha1.RegexMatch{
								Pattern: "node-[0-9]+",
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Delete(context.TODO(), hyperNodeName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should allow HyperNode creation with valid labelMatch selector", func() {
		hyperNodeName := "valid-label-match-hypernode"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							LabelMatch: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"topology-rack": "rack1",
								},
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Delete(context.TODO(), hyperNodeName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject HyperNode creation with empty exactMatch name", func() {
		hyperNodeName := "invalid-empty-exact-match"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							ExactMatch: &hypernodev1alpha1.ExactMatch{
								Name: "",
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("member exactMatch name is required"))
	})

	ginkgo.It("Should reject HyperNode creation with empty regexMatch pattern", func() {
		hyperNodeName := "invalid-empty-regex-pattern"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							RegexMatch: &hypernodev1alpha1.RegexMatch{
								Pattern: "",
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("member regexMatch pattern is required"))
	})

	ginkgo.It("Should reject HyperNode creation with invalid regex pattern", func() {
		hyperNodeName := "invalid-regex-pattern"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							RegexMatch: &hypernodev1alpha1.RegexMatch{
								Pattern: "a(b",
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("member regexMatch pattern is invalid"))
	})

	ginkgo.It("Should reject HyperNode creation with no members", func() {
		hyperNodeName := "invalid-no-members"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier:    1,
				Members: []hypernodev1alpha1.MemberSpec{},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("member must have at least one member"))
	})

	ginkgo.It("Should reject HyperNode creation with invalid exactMatch name (invalid DNS name)", func() {
		hyperNodeName := "invalid-dns-name"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							ExactMatch: &hypernodev1alpha1.ExactMatch{
								Name: "Node_with_invalid@characters",
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("member exactMatch validate failed"))
	})

	ginkgo.It("Should allow HyperNode update with valid changes", func() {
		hyperNodeName := "update-test-hypernode"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Create initial HyperNode
		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							ExactMatch: &hypernodev1alpha1.ExactMatch{
								Name: "node-1",
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Retry update with fresh object on conflict
		var updateErr error
		for retryCount := 0; retryCount < 5; retryCount++ {
			// Fetch the latest version before updating
			latestHyperNode, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), hyperNodeName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Update HyperNode with valid changes
			latestHyperNode.Spec.Members = []hypernodev1alpha1.MemberSpec{
				{
					Type: hypernodev1alpha1.MemberTypeNode,
					Selector: hypernodev1alpha1.MemberSelector{
						RegexMatch: &hypernodev1alpha1.RegexMatch{
							Pattern: "node-[1-5]",
						},
					},
				},
			}

			_, updateErr = testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Update(context.TODO(), latestHyperNode, metav1.UpdateOptions{})
			if updateErr == nil {
				break // Success
			}
			// Only retry on conflict errors
			if !errors.IsConflict(updateErr) {
				break
			}
		}
		gomega.Expect(updateErr).NotTo(gomega.HaveOccurred())

		// Cleanup
		err = testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Delete(context.TODO(), hyperNodeName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("Should reject HyperNode update with invalid changes", func() {
		hyperNodeName := "update-invalid-test-hypernode"
		testCtx := util.InitTestContext(util.Options{})
		defer util.CleanupTestContext(testCtx)

		// Create initial HyperNode
		hyperNode := &hypernodev1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: hyperNodeName,
			},
			Spec: hypernodev1alpha1.HyperNodeSpec{
				Tier: 1,
				Members: []hypernodev1alpha1.MemberSpec{
					{
						Type: hypernodev1alpha1.MemberTypeNode,
						Selector: hypernodev1alpha1.MemberSelector{
							ExactMatch: &hypernodev1alpha1.ExactMatch{
								Name: "node-1",
							},
						},
					},
				},
			},
		}

		_, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Retry update with fresh object on conflict, but expect validation error
		var updateErr error
		for retryCount := 0; retryCount < 5; retryCount++ {
			// Fetch the latest version before updating
			latestHyperNode, err := testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), hyperNodeName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Update HyperNode with invalid changes (empty members)
			latestHyperNode.Spec.Members = []hypernodev1alpha1.MemberSpec{}

			_, updateErr = testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Update(context.TODO(), latestHyperNode, metav1.UpdateOptions{})
			// If we get a conflict, retry with fresh object
			if errors.IsConflict(updateErr) {
				continue
			}
			// For any other error (including validation errors), break and check
			break
		}

		// We expect an error containing our validation message
		gomega.Expect(updateErr).To(gomega.HaveOccurred())
		gomega.Expect(updateErr.Error()).To(gomega.ContainSubstring("member must have at least one member"))

		// Cleanup
		err = testCtx.Vcclient.TopologyV1alpha1().HyperNodes().Delete(context.TODO(), hyperNodeName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
