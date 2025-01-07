package dra

import (
	"embed"

	"k8s.io/kubernetes/test/e2e/framework/testfiles"
)

//go:embed dra-test-driver-proxy.yaml
var draTestingManifestsFS embed.FS

// GetDRATestingManifestsFS is used to embed dra-test-driver-proxy.yaml.
// In the k8s 1.31 version, test/e2e/testing-manifests/embed.go does not embed dra-test-driver-proxy.yaml in the dra directory,
// and the function `SetUp` of `Driver` in "k8s.io/kubernetes/test/e2e/dra" must depend on test/e2e/testing-manifests/dra/dra-test-driver-proxy.yaml,
// see: https://github.com/kubernetes/kubernetes/blob/4391d09367e8341619f7f6d80fd8b8cb501a271f/test/e2e/dra/deploy.go#L308-#L312,
// so we need to create another embed.FS to embed dra-test-driver-proxy.yaml. This is the easiest method without modifying the source code of Driver.
func GetDRATestingManifestsFS() testfiles.EmbeddedFileSource {
	return testfiles.EmbeddedFileSource{
		EmbeddedFS: draTestingManifestsFS,
		Root:       "test/e2e/testing-manifests/dra",
	}
}
