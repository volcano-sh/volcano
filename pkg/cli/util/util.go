/*
Copyright 2019 The Volcano Authors.

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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	vcbus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	"volcano.sh/apis/pkg/client/clientset/versioned"
)

// CommonFlags are the flags that most command lines have.
type CommonFlags struct {
	Master     string
	Kubeconfig string
}

// InitFlags initializes the common flags for most command lines.
func InitFlags(cmd *cobra.Command, cf *CommonFlags) {
	cmd.Flags().StringVarP(&cf.Master, "master", "s", "", "the address of apiserver")
	cmd.Flags().StringVarP(&cf.Kubeconfig, "kubeconfig", "k", "", "(optional) absolute path to the kubeconfig file")
}

// BuildConfig builds the configuration file for command lines.
func BuildConfig(master, kubeconfig string) (*rest.Config, error) {
	// This will automatically load KUBECONFIG environment variable.
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmd.ClusterDefaults}
	if master != "" {
		overrides.ClusterInfo.Server = master
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

// PopulateResourceListV1 takes strings of form <resourceName1>=<value1>,<resourceName2>=<value2> and returns ResourceList.
func PopulateResourceListV1(spec string) (v1.ResourceList, error) {
	// empty input gets a nil response to preserve generator test expected behaviors
	if spec == "" {
		return nil, nil
	}

	result := v1.ResourceList{}
	resourceStatements := strings.Split(spec, ",")
	for _, resourceStatement := range resourceStatements {
		parts := strings.Split(resourceStatement, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid argument syntax %v, expected <resource>=<value>", resourceStatement)
		}
		resourceName := v1.ResourceName(parts[0])
		resourceQuantity, err := resource.ParseQuantity(parts[1])
		if err != nil {
			return nil, err
		}
		result[resourceName] = resourceQuantity
	}
	return result, nil
}

// CreateQueueCommand executes a command such as open/close
func CreateQueueCommand(vcClient *versioned.Clientset, ns, name string, action vcbus.Action) error {
	queue, err := vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	ctrlRef := metav1.NewControllerRef(queue, helpers.V1beta1QueueKind)
	cmd := &vcbus.Command{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-",
				queue.Name, strings.ToLower(string(action))),
			Namespace: queue.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*ctrlRef,
			},
		},
		TargetObject: ctrlRef,
		Action:       string(action),
	}

	if _, err := vcClient.BusV1alpha1().Commands(ns).Create(context.TODO(), cmd, metav1.CreateOptions{}); err != nil {
		return err
	}

	return nil
}

// CreateJobCommand executes a command such as resume/suspend.
func CreateJobCommand(ctx context.Context, config *rest.Config, ns, name string, action vcbus.Action) error {
	jobClient := versioned.NewForConfigOrDie(config)
	job, err := jobClient.BatchV1alpha1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	ctrlRef := metav1.NewControllerRef(job, helpers.JobKind)
	cmd := &vcbus.Command{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-",
				job.Name, strings.ToLower(string(action))),
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*ctrlRef,
			},
		},
		TargetObject: ctrlRef,
		Action:       string(action),
	}

	if _, err := jobClient.BusV1alpha1().Commands(ns).Create(ctx, cmd, metav1.CreateOptions{}); err != nil {
		return err
	}

	return nil
}

// TranslateTimestampSince translates the time stamp.
func TranslateTimestampSince(timestamp metav1.Time) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}
	return HumanDuration(time.Since(timestamp.Time))
}

// HumanDuration translate time.Duration to human readable time string.
func HumanDuration(d time.Duration) string {
	// Allow deviation no more than 2 seconds(excluded) to tolerate machine time
	// inconsistence, it can be considered as almost now.
	if seconds := int(d.Seconds()); seconds < -1 {
		return "<invalid>"
	} else if seconds < 0 {
		return "0s"
	} else if seconds < 60*2 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := int(d / time.Minute)
	if minutes < 10 {
		s := int(d/time.Second) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, s)
	} else if minutes < 60*3 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := int(d / time.Hour)
	if hours < 8 {
		m := int(d/time.Minute) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, m)
	} else if hours < 48 {
		return fmt.Sprintf("%dh", hours)
	} else if hours < 24*8 {
		h := hours % 24
		if h == 0 {
			return fmt.Sprintf("%dd", hours/24)
		}
		return fmt.Sprintf("%dd%dh", hours/24, h)
	} else if hours < 24*365*2 {
		return fmt.Sprintf("%dd", hours/24)
	} else if hours < 24*365*8 {
		return fmt.Sprintf("%dy%dd", hours/24/365, (hours/24)%365)
	}
	return fmt.Sprintf("%dy", hours/24/365)
}

// RedirectStdout redirects os.Stdout to a pipe and returns the read and write ends of the pipe.
func RedirectStdout() (*os.File, *os.File) {
	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	return r, oldStdout
}

// CaptureOutput reads from r until EOF and returns the result as a string.
func CaptureOutput(r *os.File, oldStdout *os.File) string {
	w := os.Stdout
	os.Stdout = oldStdout
	w.Close()
	gotOutput, _ := io.ReadAll(r)
	return strings.TrimSpace(string(gotOutput))
}

// CreateTestServer creates an HTTP server that responds with the given response.
func CreateTestServer(response interface{}) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		val, err := json.Marshal(response)
		if err == nil {
			w.Write(val)
		}
	})

	server := httptest.NewServer(handler)
	return server
}
