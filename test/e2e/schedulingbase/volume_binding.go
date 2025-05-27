/*
Copyright 2018 The Kubernetes Authors.

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

package schedulingbase

// This file is copied from kubernetes test/e2e/storage/persistent_volume-local.go and make some changes.

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	e2epv "k8s.io/kubernetes/test/e2e/framework/pv"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	e2evolume "k8s.io/kubernetes/test/e2e/framework/volume"
	"k8s.io/kubernetes/test/e2e/storage/utils"
	imageutils "k8s.io/kubernetes/test/utils/image"
	admissionapi "k8s.io/pod-security-admission/api"

	volcanotestutil "volcano.sh/volcano/test/e2e/util"
)

type localTestConfig struct {
	ns           string
	nodes        []v1.Node
	randomNode   *v1.Node
	client       clientset.Interface
	timeouts     *framework.TimeoutContext
	scName       string
	discoveryDir string
	hostExec     utils.HostExec
	ltrMgr       utils.LocalTestResourceManager
}

type localVolumeType string

const (
	// DirectoryLocalVolumeType is the default local volume type, aka a directory
	DirectoryLocalVolumeType localVolumeType = "dir"
)

// map to local test resource type
var setupLocalVolumeMap = map[localVolumeType]utils.LocalVolumeType{
	DirectoryLocalVolumeType: utils.LocalVolumeDirectory,
}

type localTestVolume struct {
	// Local test resource
	ltr *utils.LocalTestResource
	// PVC for this volume
	pvc *v1.PersistentVolumeClaim
	// PV for this volume
	pv *v1.PersistentVolume
	// Type of local volume
	localVolumeType localVolumeType
}

const (
	// TODO: This may not be available/writable on all images.
	hostBase = "/tmp"
	// Path to the first volume in the test containers
	// created via createLocalPod or makeLocalPod
	// leveraging pv_util.MakePod
	volumeDir = "/mnt/volume1"
	// testFile created in setupLocalVolume
	testFile = "test-file"
	// testFileContent written into testFile
	testFileContent = "test-file-content"
	testSCPrefix    = "local-volume-test-storageclass"

	// A sample request size
	testRequestSize = "10Mi"

	// Max number of nodes to use for testing
	maxNodes = 5
)

var (
	// storage class volume binding modes
	waitMode      = storagev1.VolumeBindingWaitForFirstConsumer
	immediateMode = storagev1.VolumeBindingImmediate

	// Common selinux labels
	selinuxLabel = &v1.SELinuxOptions{
		Level: "s0:c0,c1"}
)

var _ = ginkgo.Describe("Volume Binding Test", func() {
	f := framework.NewDefaultFramework("volume-binding")
	f.NamespacePodSecurityLevel = admissionapi.LevelPrivileged

	var (
		config *localTestConfig
		scName string
	)

	ginkgo.BeforeEach(func(ctx context.Context) {
		nodes, err := e2enode.GetBoundedReadySchedulableNodes(ctx, f.ClientSet, maxNodes)
		framework.ExpectNoError(err)

		scName = fmt.Sprintf("%v-%v", testSCPrefix, f.Namespace.Name)
		// Choose a random node
		randomNode := &nodes.Items[rand.Intn(len(nodes.Items))]

		hostExec := utils.NewHostExec(f)
		ltrMgr := utils.NewLocalResourceManager("local-volume-test", hostExec, hostBase)
		config = &localTestConfig{
			ns:           f.Namespace.Name,
			client:       f.ClientSet,
			timeouts:     f.Timeouts,
			nodes:        nodes.Items,
			randomNode:   randomNode,
			scName:       scName,
			discoveryDir: filepath.Join(hostBase, f.Namespace.Name),
			hostExec:     hostExec,
			ltrMgr:       ltrMgr,
		}
	})

	for tempTestVolType := range setupLocalVolumeMap {

		// New variable required for ginkgo test closures
		testVolType := tempTestVolType
		args := []interface{}{fmt.Sprintf("[Volume type: %s]", testVolType)}
		testMode := immediateMode

		args = append(args, func() {
			var testVol *localTestVolume

			ginkgo.BeforeEach(func(ctx context.Context) {
				setupStorageClass(ctx, config, &testMode)
				testVols := setupLocalVolumesPVCsPVs(ctx, config, testVolType, config.randomNode, 1, testMode)
				if len(testVols) > 0 {
					testVol = testVols[0]
				} else {
					framework.Failf("Failed to get a test volume")
				}
			})

			ginkgo.AfterEach(func(ctx context.Context) {
				if testVol != nil {
					cleanupLocalVolumes(ctx, config, []*localTestVolume{testVol})
					cleanupStorageClass(ctx, config)
				} else {
					framework.Failf("no test volume to cleanup")
				}
			})

			ginkgo.Context("One pod requesting one prebound PVC", func() {
				var (
					pod1    *v1.Pod
					pod1Err error
				)

				ginkgo.BeforeEach(func(ctx context.Context) {
					ginkgo.By("Creating pod1")
					pod1, pod1Err = createLocalPod(ctx, config, testVol, nil)
					framework.ExpectNoError(pod1Err)
					verifyLocalPod(ctx, config, testVol, pod1, config.randomNode.Name)

					writeCmd := createWriteCmd(volumeDir, testFile, testFileContent, testVol.localVolumeType)

					ginkgo.By("Writing in pod1")
					podRWCmdExec(f, pod1, writeCmd)
				})

				ginkgo.AfterEach(func(ctx context.Context) {
					ginkgo.By("Deleting pod1")
					e2epod.DeletePodOrFail(ctx, config.client, config.ns, pod1.Name)
				})

				ginkgo.It("should be able to mount volume and read from pod1", func(ctx context.Context) {
					ginkgo.By("Reading in pod1")
					// testFileContent was written in BeforeEach
					testReadFileContent(f, volumeDir, testFile, testFileContent, pod1, testVolType)
				})

				ginkgo.It("should be able to mount volume and write from pod1", func(ctx context.Context) {
					// testFileContent was written in BeforeEach
					testReadFileContent(f, volumeDir, testFile, testFileContent, pod1, testVolType)

					ginkgo.By("Writing in pod1")
					writeCmd := createWriteCmd(volumeDir, testFile, testVol.ltr.Path /*writeTestFileContent*/, testVolType)
					podRWCmdExec(f, pod1, writeCmd)
				})
			})

			ginkgo.Context("Two pods mounting a local volume at the same time", func() {
				ginkgo.It("should be able to write from pod1 and read from pod2", func(ctx context.Context) {
					twoPodsReadWriteTest(ctx, f, config, testVol)
				})
			})

			ginkgo.Context("Two pods mounting a local volume one after the other", func() {
				ginkgo.It("should be able to write from pod1 and read from pod2", func(ctx context.Context) {
					twoPodsReadWriteSerialTest(ctx, f, config, testVol)
				})
			})
		})
		f.Context(args...)
	}

	f.Context("Local volume that cannot be mounted", f.WithSlow(), func() {
		ginkgo.It("should fail due to non-existent path", func(ctx context.Context) {
			testVol := &localTestVolume{
				ltr: &utils.LocalTestResource{
					Node: config.randomNode,
					Path: "/non-existent/location/nowhere",
				},
				localVolumeType: DirectoryLocalVolumeType,
			}
			ginkgo.By("Creating local PVC and PV")
			createLocalPVCsPVs(ctx, config, []*localTestVolume{testVol}, immediateMode)
			// createLocalPod will create a pod and wait for it to be running. In this case,
			// It's expected that the Pod fails to start.
			_, err := createLocalPod(ctx, config, testVol, nil)
			gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("is not Running")))
			cleanupLocalPVCsPVs(ctx, config, []*localTestVolume{testVol})
		})

		ginkgo.It("should fail due to wrong node", func(ctx context.Context) {
			if len(config.nodes) < 2 {
				e2eskipper.Skipf("Runs only when number of nodes >= 2")
			}

			testVols := setupLocalVolumesPVCsPVs(ctx, config, DirectoryLocalVolumeType, config.randomNode, 1, immediateMode)
			testVol := testVols[0]

			conflictNodeName := config.nodes[0].Name
			if conflictNodeName == config.randomNode.Name {
				conflictNodeName = config.nodes[1].Name
			}
			pod := makeLocalPodWithNodeName(config, testVol, conflictNodeName)
			pod, err := config.client.CoreV1().Pods(config.ns).Create(ctx, pod, metav1.CreateOptions{})
			framework.ExpectNoError(err)

			getPod := e2epod.Get(f.ClientSet, pod)
			gomega.Consistently(ctx, getPod, f.Timeouts.PodStart, 2*time.Second).ShouldNot(e2epod.BeInPhase(v1.PodRunning))

			cleanupLocalVolumes(ctx, config, []*localTestVolume{testVol})
		})
	})

	ginkgo.Context("Pod with node different from PV's NodeAffinity", func() {
		var (
			testVol          *localTestVolume
			volumeType       localVolumeType
			conflictNodeName string
		)

		ginkgo.BeforeEach(func(ctx context.Context) {
			if len(config.nodes) < 2 {
				e2eskipper.Skipf("Runs only when number of nodes >= 2")
			}

			volumeType = DirectoryLocalVolumeType
			setupStorageClass(ctx, config, &immediateMode)
			testVols := setupLocalVolumesPVCsPVs(ctx, config, volumeType, config.randomNode, 1, immediateMode)
			conflictNodeName = config.nodes[0].Name
			if conflictNodeName == config.randomNode.Name {
				conflictNodeName = config.nodes[1].Name
			}

			testVol = testVols[0]
		})

		ginkgo.AfterEach(func(ctx context.Context) {
			cleanupLocalVolumes(ctx, config, []*localTestVolume{testVol})
			cleanupStorageClass(ctx, config)
		})

		ginkgo.It("should fail scheduling due to different NodeAffinity", func(ctx context.Context) {
			testPodWithNodeConflict(ctx, config, testVol, conflictNodeName, makeLocalPodWithNodeAffinity)
		})

		ginkgo.It("should fail scheduling due to different NodeSelector", func(ctx context.Context) {
			testPodWithNodeConflict(ctx, config, testVol, conflictNodeName, makeLocalPodWithNodeSelector)
		})
	})

	f.Context("StatefulSet with pod affinity", f.WithSlow(), func() {
		var testVols map[string][]*localTestVolume
		const (
			ssReplicas  = 3
			volsPerNode = 6
		)

		ginkgo.BeforeEach(func(ctx context.Context) {
			setupStorageClass(ctx, config, &waitMode)

			testVols = map[string][]*localTestVolume{}
			for i, node := range config.nodes {
				// The PVCs created here won't be used
				ginkgo.By(fmt.Sprintf("Setting up local volumes on node %q", node.Name))
				vols := setupLocalVolumesPVCsPVs(ctx, config, DirectoryLocalVolumeType, &config.nodes[i], volsPerNode, waitMode)
				testVols[node.Name] = vols
			}
		})

		ginkgo.AfterEach(func(ctx context.Context) {
			for _, vols := range testVols {
				cleanupLocalVolumes(ctx, config, vols)
			}
			cleanupStorageClass(ctx, config)
		})

		ginkgo.It("should use volumes spread across nodes when pod has anti-affinity", func(ctx context.Context) {
			if len(config.nodes) < ssReplicas {
				e2eskipper.Skipf("Runs only when number of nodes >= %v", ssReplicas)
			}
			ginkgo.By("Creating a StatefulSet with pod anti-affinity on nodes")
			ss := createStatefulSet(ctx, config, ssReplicas, volsPerNode, true, false)
			validateStatefulSet(ctx, config, ss, true)
		})

		ginkgo.It("should use volumes on one node when pod has affinity", func(ctx context.Context) {
			ginkgo.By("Creating a StatefulSet with pod affinity on nodes")
			ss := createStatefulSet(ctx, config, ssReplicas, volsPerNode/ssReplicas, false, false)
			validateStatefulSet(ctx, config, ss, false)
		})

		ginkgo.It("should use volumes spread across nodes when pod management is parallel and pod has anti-affinity", func(ctx context.Context) {
			if len(config.nodes) < ssReplicas {
				e2eskipper.Skipf("Runs only when number of nodes >= %v", ssReplicas)
			}
			ginkgo.By("Creating a StatefulSet with pod anti-affinity on nodes")
			ss := createStatefulSet(ctx, config, ssReplicas, 1, true, true)
			validateStatefulSet(ctx, config, ss, true)
		})

		ginkgo.It("should use volumes on one node when pod management is parallel and pod has affinity", func(ctx context.Context) {
			ginkgo.By("Creating a StatefulSet with pod affinity on nodes")
			ss := createStatefulSet(ctx, config, ssReplicas, 1, false, true)
			validateStatefulSet(ctx, config, ss, false)
		})
	})
})

type makeLocalPodWith func(config *localTestConfig, volume *localTestVolume, nodeName string) *v1.Pod

func testPodWithNodeConflict(ctx context.Context, config *localTestConfig, testVol *localTestVolume, nodeName string, makeLocalPodFunc makeLocalPodWith) {
	ginkgo.By(fmt.Sprintf("local-volume-type: %s", testVol.localVolumeType))

	pod := makeLocalPodFunc(config, testVol, nodeName)
	pod, err := config.client.CoreV1().Pods(config.ns).Create(ctx, pod, metav1.CreateOptions{})
	framework.ExpectNoError(err)

	err = e2epod.WaitForPodNameUnschedulableInNamespace(ctx, config.client, pod.Name, pod.Namespace)
	framework.ExpectNoError(err)
}

// The tests below are run against multiple mount point types

// Test two pods at the same time, write from pod1, and read from pod2
func twoPodsReadWriteTest(ctx context.Context, f *framework.Framework, config *localTestConfig, testVol *localTestVolume) {
	ginkgo.By("Creating pod1 to write to the PV")
	pod1, pod1Err := createLocalPod(ctx, config, testVol, nil)
	framework.ExpectNoError(pod1Err)
	verifyLocalPod(ctx, config, testVol, pod1, config.randomNode.Name)

	writeCmd := createWriteCmd(volumeDir, testFile, testFileContent, testVol.localVolumeType)

	ginkgo.By("Writing in pod1")
	podRWCmdExec(f, pod1, writeCmd)

	// testFileContent was written after creating pod1
	testReadFileContent(f, volumeDir, testFile, testFileContent, pod1, testVol.localVolumeType)

	ginkgo.By("Creating pod2 to read from the PV")
	pod2, pod2Err := createLocalPod(ctx, config, testVol, nil)
	framework.ExpectNoError(pod2Err)
	verifyLocalPod(ctx, config, testVol, pod2, config.randomNode.Name)

	// testFileContent was written after creating pod1
	testReadFileContent(f, volumeDir, testFile, testFileContent, pod2, testVol.localVolumeType)

	writeCmd = createWriteCmd(volumeDir, testFile, testVol.ltr.Path /*writeTestFileContent*/, testVol.localVolumeType)

	ginkgo.By("Writing in pod2")
	podRWCmdExec(f, pod2, writeCmd)

	ginkgo.By("Reading in pod1")
	testReadFileContent(f, volumeDir, testFile, testVol.ltr.Path, pod1, testVol.localVolumeType)

	ginkgo.By("Deleting pod1")
	e2epod.DeletePodOrFail(ctx, config.client, config.ns, pod1.Name)
	ginkgo.By("Deleting pod2")
	e2epod.DeletePodOrFail(ctx, config.client, config.ns, pod2.Name)
}

// Test two pods one after other, write from pod1, and read from pod2
func twoPodsReadWriteSerialTest(ctx context.Context, f *framework.Framework, config *localTestConfig, testVol *localTestVolume) {
	ginkgo.By("Creating pod1")
	pod1, pod1Err := createLocalPod(ctx, config, testVol, nil)
	framework.ExpectNoError(pod1Err)
	verifyLocalPod(ctx, config, testVol, pod1, config.randomNode.Name)

	writeCmd := createWriteCmd(volumeDir, testFile, testFileContent, testVol.localVolumeType)

	ginkgo.By("Writing in pod1")
	podRWCmdExec(f, pod1, writeCmd)

	// testFileContent was written after creating pod1
	testReadFileContent(f, volumeDir, testFile, testFileContent, pod1, testVol.localVolumeType)

	ginkgo.By("Deleting pod1")
	e2epod.DeletePodOrFail(ctx, config.client, config.ns, pod1.Name)

	ginkgo.By("Creating pod2")
	pod2, pod2Err := createLocalPod(ctx, config, testVol, nil)
	framework.ExpectNoError(pod2Err)
	verifyLocalPod(ctx, config, testVol, pod2, config.randomNode.Name)

	ginkgo.By("Reading in pod2")
	testReadFileContent(f, volumeDir, testFile, testFileContent, pod2, testVol.localVolumeType)

	ginkgo.By("Deleting pod2")
	e2epod.DeletePodOrFail(ctx, config.client, config.ns, pod2.Name)
}

func setupStorageClass(ctx context.Context, config *localTestConfig, mode *storagev1.VolumeBindingMode) {
	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: config.scName,
		},
		Provisioner:       "kubernetes.io/no-provisioner",
		VolumeBindingMode: mode,
	}

	_, err := config.client.StorageV1().StorageClasses().Create(ctx, sc, metav1.CreateOptions{})
	framework.ExpectNoError(err)
}

func cleanupStorageClass(ctx context.Context, config *localTestConfig) {
	framework.ExpectNoError(config.client.StorageV1().StorageClasses().Delete(ctx, config.scName, metav1.DeleteOptions{}))
}

// podNode wraps RunKubectl to get node where pod is running
func podNodeName(ctx context.Context, config *localTestConfig, pod *v1.Pod) (string, error) {
	runtimePod, runtimePodErr := config.client.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	return runtimePod.Spec.NodeName, runtimePodErr
}

// setupLocalVolumes sets up directories to use for local PV
func setupLocalVolumes(ctx context.Context, config *localTestConfig, localVolumeType localVolumeType, node *v1.Node, count int) []*localTestVolume {
	vols := []*localTestVolume{}
	for i := 0; i < count; i++ {
		ltrType, ok := setupLocalVolumeMap[localVolumeType]
		if !ok {
			framework.Failf("Invalid localVolumeType: %v", localVolumeType)
		}
		ltr := config.ltrMgr.Create(ctx, node, ltrType, nil)
		vols = append(vols, &localTestVolume{
			ltr:             ltr,
			localVolumeType: localVolumeType,
		})
	}
	return vols
}

func cleanupLocalPVCsPVs(ctx context.Context, config *localTestConfig, volumes []*localTestVolume) {
	for _, volume := range volumes {
		ginkgo.By("Cleaning up PVC and PV")
		errs := e2epv.PVPVCCleanup(ctx, config.client, config.ns, volume.pv, volume.pvc)
		if len(errs) > 0 {
			framework.Failf("Failed to delete PV and/or PVC: %v", utilerrors.NewAggregate(errs))
		}
	}
}

// Deletes the PVC/PV, and launches a pod with hostpath volume to remove the test directory
func cleanupLocalVolumes(ctx context.Context, config *localTestConfig, volumes []*localTestVolume) {
	cleanupLocalPVCsPVs(ctx, config, volumes)

	for _, volume := range volumes {
		config.ltrMgr.Remove(ctx, volume.ltr)
	}
}

func verifyLocalVolume(ctx context.Context, config *localTestConfig, volume *localTestVolume) {
	framework.ExpectNoError(e2epv.WaitOnPVandPVC(ctx, config.client, config.timeouts, config.ns, volume.pv, volume.pvc))
}

func verifyLocalPod(ctx context.Context, config *localTestConfig, volume *localTestVolume, pod *v1.Pod, expectedNodeName string) {
	podNodeName, err := podNodeName(ctx, config, pod)
	framework.ExpectNoError(err)
	framework.Logf("pod %q created on Node %q", pod.Name, podNodeName)
	gomega.Expect(podNodeName).To(gomega.Equal(expectedNodeName))
}

func makeLocalPVCConfig(config *localTestConfig, volumeType localVolumeType) e2epv.PersistentVolumeClaimConfig {
	pvcConfig := e2epv.PersistentVolumeClaimConfig{
		AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
		StorageClassName: &config.scName,
	}
	return pvcConfig
}

func makeLocalPVConfig(config *localTestConfig, volume *localTestVolume) e2epv.PersistentVolumeConfig {
	// TODO: hostname may not be the best option
	nodeKey := "kubernetes.io/hostname"
	if volume.ltr.Node.Labels == nil {
		framework.Failf("Node does not have labels")
	}
	nodeValue, found := volume.ltr.Node.Labels[nodeKey]
	if !found {
		framework.Failf("Node does not have required label %q", nodeKey)
	}

	pvConfig := e2epv.PersistentVolumeConfig{
		PVSource: v1.PersistentVolumeSource{
			Local: &v1.LocalVolumeSource{
				Path: volume.ltr.Path,
			},
		},
		NamePrefix:       "local-pv",
		StorageClassName: config.scName,
		NodeAffinity: &v1.VolumeNodeAffinity{
			Required: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      nodeKey,
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{nodeValue},
							},
						},
					},
				},
			},
		},
	}

	return pvConfig
}

// Creates a PVC and PV with prebinding
func createLocalPVCsPVs(ctx context.Context, config *localTestConfig, volumes []*localTestVolume, mode storagev1.VolumeBindingMode) {
	var err error

	for _, volume := range volumes {
		pvcConfig := makeLocalPVCConfig(config, volume.localVolumeType)
		pvConfig := makeLocalPVConfig(config, volume)

		volume.pv, volume.pvc, err = e2epv.CreatePVPVC(ctx, config.client, config.timeouts, pvConfig, pvcConfig, config.ns, false)
		framework.ExpectNoError(err)
	}

	if mode == storagev1.VolumeBindingImmediate {
		for _, volume := range volumes {
			verifyLocalVolume(ctx, config, volume)
		}
	} else {
		// Verify PVCs are not bound by waiting for phase==bound with a timeout and asserting that we hit the timeout.
		// There isn't really a great way to verify this without making the test be slow...
		const bindTimeout = 10 * time.Second
		waitErr := wait.PollImmediate(time.Second, bindTimeout, func() (done bool, err error) {
			for _, volume := range volumes {
				pvc, err := config.client.CoreV1().PersistentVolumeClaims(volume.pvc.Namespace).Get(ctx, volume.pvc.Name, metav1.GetOptions{})
				if err != nil {
					return false, fmt.Errorf("failed to get PVC %s/%s: %w", volume.pvc.Namespace, volume.pvc.Name, err)
				}
				if pvc.Status.Phase != v1.ClaimPending {
					return true, nil
				}
			}
			return false, nil
		})
		if wait.Interrupted(waitErr) {
			framework.Logf("PVCs were not bound within %v (that's good)", bindTimeout)
			waitErr = nil
		}
		framework.ExpectNoError(waitErr, "Error making sure PVCs are not bound")
	}
}

func makeLocalPodWithNodeAffinity(config *localTestConfig, volume *localTestVolume, nodeName string) (pod *v1.Pod) {
	affinity := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{nodeName},
							},
						},
					},
				},
			},
		},
	}
	podConfig := e2epod.Config{
		NS:            config.ns,
		PVCs:          []*v1.PersistentVolumeClaim{volume.pvc},
		SeLinuxLabel:  selinuxLabel,
		NodeSelection: e2epod.NodeSelection{Affinity: affinity},
	}
	pod, err := e2epod.MakeSecPod(&podConfig)
	if pod == nil || err != nil {
		return
	}
	pod.Spec.Affinity = affinity
	return
}

func makeLocalPodWithNodeSelector(config *localTestConfig, volume *localTestVolume, nodeName string) (pod *v1.Pod) {
	ns := map[string]string{
		"kubernetes.io/hostname": nodeName,
	}
	podConfig := e2epod.Config{
		NS:            config.ns,
		PVCs:          []*v1.PersistentVolumeClaim{volume.pvc},
		SeLinuxLabel:  selinuxLabel,
		NodeSelection: e2epod.NodeSelection{Selector: ns},
	}
	pod, err := e2epod.MakeSecPod(&podConfig)
	if pod == nil || err != nil {
		return
	}
	return
}

func makeLocalPodWithNodeName(config *localTestConfig, volume *localTestVolume, nodeName string) (pod *v1.Pod) {
	podConfig := e2epod.Config{
		NS:           config.ns,
		PVCs:         []*v1.PersistentVolumeClaim{volume.pvc},
		SeLinuxLabel: selinuxLabel,
	}
	pod, err := e2epod.MakeSecPod(&podConfig)
	if pod == nil || err != nil {
		return
	}

	e2epod.SetNodeAffinity(&pod.Spec, nodeName)
	return
}

func createLocalPod(ctx context.Context, config *localTestConfig, volume *localTestVolume, fsGroup *int64) (*v1.Pod, error) {
	ginkgo.By("Creating a pod")
	podConfig := e2epod.Config{
		NS:           config.ns,
		PVCs:         []*v1.PersistentVolumeClaim{volume.pvc},
		SeLinuxLabel: selinuxLabel,
		FsGroup:      fsGroup,
	}
	return e2epod.CreateSecPod(ctx, config.client, &podConfig, config.timeouts.PodStart)
}

func createWriteCmd(testDir string, testFile string, writeTestFileContent string, volumeType localVolumeType) string {
	testFilePath := filepath.Join(testDir, testFile)
	return fmt.Sprintf("mkdir -p %s; echo %s > %s", testDir, writeTestFileContent, testFilePath)
}

func createReadCmd(testFileDir string, testFile string, volumeType localVolumeType) string {
	// Create the command to read (aka cat) a file.
	testFilePath := filepath.Join(testFileDir, testFile)
	return fmt.Sprintf("cat %s", testFilePath)
}

// Read testFile and evaluate whether it contains the testFileContent
func testReadFileContent(f *framework.Framework, testFileDir string, testFile string, testFileContent string, pod *v1.Pod, volumeType localVolumeType) {
	readCmd := createReadCmd(testFileDir, testFile, volumeType)
	readOut := podRWCmdExec(f, pod, readCmd)
	gomega.Expect(readOut).To(gomega.ContainSubstring(testFileContent))
}

// Execute a read or write command in a pod.
// Fail on error
func podRWCmdExec(f *framework.Framework, pod *v1.Pod, cmd string) string {
	stdout, stderr, err := e2evolume.PodExec(f, pod, cmd)
	framework.Logf("podRWCmdExec cmd: %q, out: %q, stderr: %q, err: %v", cmd, stdout, stderr, err)
	framework.ExpectNoError(err)
	return stdout
}

// Initialize test volume on node
// and create local PVC and PV
func setupLocalVolumesPVCsPVs(
	ctx context.Context,
	config *localTestConfig,
	localVolumeType localVolumeType,
	node *v1.Node,
	count int,
	mode storagev1.VolumeBindingMode) []*localTestVolume {

	ginkgo.By("Initializing test volumes")
	testVols := setupLocalVolumes(ctx, config, localVolumeType, node, count)

	ginkgo.By("Creating local PVCs and PVs")
	createLocalPVCsPVs(ctx, config, testVols, mode)

	return testVols
}

// newLocalClaim creates a new persistent volume claim.
func newLocalClaimWithName(config *localTestConfig, name string) *v1.PersistentVolumeClaim {
	claim := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: config.ns,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			StorageClassName: &config.scName,
			AccessModes: []v1.PersistentVolumeAccessMode{
				v1.ReadWriteOnce,
			},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(v1.ResourceStorage): resource.MustParse(testRequestSize),
				},
			},
		},
	}

	return &claim
}

func createStatefulSet(ctx context.Context, config *localTestConfig, ssReplicas int32, volumeCount int, anti, parallel bool) *appsv1.StatefulSet {
	mounts := []v1.VolumeMount{}
	claims := []v1.PersistentVolumeClaim{}
	for i := 0; i < volumeCount; i++ {
		name := fmt.Sprintf("vol%v", i+1)
		pvc := newLocalClaimWithName(config, name)
		mounts = append(mounts, v1.VolumeMount{Name: name, MountPath: "/" + name})
		claims = append(claims, *pvc)
	}

	podAffinityTerms := []v1.PodAffinityTerm{
		{
			LabelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "app",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"local-volume-test"},
					},
				},
			},
			TopologyKey: "kubernetes.io/hostname",
		},
	}

	affinity := v1.Affinity{}
	if anti {
		affinity.PodAntiAffinity = &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: podAffinityTerms,
		}
	} else {
		affinity.PodAffinity = &v1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: podAffinityTerms,
		}
	}

	labels := map[string]string{"app": "local-volume-test"}
	spec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local-volume-statefulset",
			Namespace: config.ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "local-volume-test"},
			},
			Replicas: &ssReplicas,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:         "nginx",
							Image:        imageutils.GetE2EImage(imageutils.Nginx),
							VolumeMounts: mounts,
						},
					},
					Affinity: &affinity,
				},
			},
			VolumeClaimTemplates: claims,
			ServiceName:          "test-service",
		},
	}

	if parallel {
		spec.Spec.PodManagementPolicy = appsv1.ParallelPodManagement
	}

	ss, err := config.client.AppsV1().StatefulSets(config.ns).Create(ctx, spec, metav1.CreateOptions{})
	framework.ExpectNoError(err)

	volcanotestutil.WaitForRunningAndReady(ctx, config.client, ssReplicas, ss)
	return ss
}

func validateStatefulSet(ctx context.Context, config *localTestConfig, ss *appsv1.StatefulSet, anti bool) {
	pods := volcanotestutil.GetPodList(ctx, config.client, ss.Namespace, ss.Spec.Selector)

	nodes := sets.NewString()
	for _, pod := range pods.Items {
		nodes.Insert(pod.Spec.NodeName)
	}

	if anti {
		// Verify that each pod is on a different node
		gomega.Expect(pods.Items).To(gomega.HaveLen(nodes.Len()))
	} else {
		// Verify that all pods are on same node.
		gomega.Expect(nodes.Len()).To(gomega.Equal(1))
	}

	// Validate all PVCs are bound
	for _, pod := range pods.Items {
		for _, volume := range pod.Spec.Volumes {
			pvcSource := volume.VolumeSource.PersistentVolumeClaim
			if pvcSource != nil {
				err := e2epv.WaitForPersistentVolumeClaimPhase(ctx,
					v1.ClaimBound, config.client, config.ns, pvcSource.ClaimName, framework.Poll, time.Second)
				framework.ExpectNoError(err)
			}
		}
	}
}
