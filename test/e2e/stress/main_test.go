/*
Copyright 2021 The Volcano Authors.

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

package stress

import (
	"os"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

func TestMain(m *testing.M) {
	home := e2eutil.HomeDir()
	configPath := e2eutil.KubeconfigPath(home)
	config, _ := clientcmd.BuildConfigFromFlags(e2eutil.MasterURL(), configPath)
	e2eutil.VcClient = vcclient.NewForConfigOrDie(config)
	e2eutil.KubeClient = kubernetes.NewForConfigOrDie(config)
	os.Exit(m.Run())
}
