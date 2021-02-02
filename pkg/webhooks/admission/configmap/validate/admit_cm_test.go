package validate

import (
	"k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"
)

func TestValidatePod(t *testing.T) {
	testCases := []struct {
		Name           string
		ConfigMap      v1.ConfigMap
		ExpectErr      bool
		reviewResponse v1beta1.AdmissionResponse
		ret            string
	}{
		{
			Name: "check whether it is volcano config",
			ConfigMap: v1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-err-name.conf",
				},
				Data: map[string]string{
					"default-scheduler.conf": `
                        actions: "enqueue, allocate, backfill"
                        tiers:
                        - plugins:
                          - name: priority
                          - name: gang
                          - name: conformance
                        - plugins:
                          - name: drf
                          - name: predicates
                          - name: proportion
                          - name: nodeorder
                        `,
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name: "check whether it exists more one config data",
			ConfigMap: v1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: configmapName,
				},
				Data: map[string]string{
					"default-scheduler.conf": `
                        actions: "enqueue, allocate, backfill"
                        tiers:
                        - plugins:
                          - name: priority
                          - name: gang
                          - name: conformance
                        - plugins:
                          - name: drf
                          - name: predicates
                          - name: proportion
                          - name: nodeorder
                    `,
					"ief-scheduler.conf": `
                        actions: "enqueue, allocate, backfill"
                        tiers:
                        - plugins:
                          - name: priority
                          - name: gang
                          - name: conformance
                        - plugins:
                          - name: drf
                          - name: predicates
                          - name: proportion
                          - name: nodeorder
                    `,
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: false},
			ret:            "data format is err",
			ExpectErr:      true,
		},
		{
			Name: "check whether it is volcano config",
			ConfigMap: v1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: configmapName,
				},
				Data: map[string]string{
					"default-scheduler.conf": `
                        actions: "enqueue, allocate, backfill"
                        configurations:
                        - name: enqueue
                          arguments:
                            overcommit-factor: 1.2
                        tiers:
                        - plugins:
                          - name: priority
                          - name: gang
                          - name: conformance
                        - plugins:
                          - name: drf
                          - name: predicates
                          - name: proportion
                          - name: nodeorder
                        `,
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: true},
			ret:            "",
			ExpectErr:      false,
		},
		{
			Name: "check whether it is volcano config",
			ConfigMap: v1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: configmapName,
				},
				Data: map[string]string{
					"default-scheduler.conf": `
                        actions: "enqueue, allocate, backfill"
                        configurations:
                        - name: enqueue
                          arguments:
                            overcommit-factor: 0.8
                        tiers:
                        - plugins:
                          - name: priority
                          - name: gang
                          - name: conformance
                        - plugins:
                          - name: drf
                          - name: predicates
                          - name: proportion
                          - name: nodeorder
                        `,
				},
			},
			reviewResponse: v1beta1.AdmissionResponse{Allowed: false},
			ret:            "must be more than",
			ExpectErr:      true,
		},
	}

	for _, testCase := range testCases {
		ret := validateConfigmap(&testCase.ConfigMap, &testCase.reviewResponse)
		if testCase.ExpectErr == true && ret == "" {
			t.Errorf("%s: test case Expect error msg :%s, but got nil.", testCase.Name, testCase.ret)
		}

		if testCase.ExpectErr == true && testCase.reviewResponse.Allowed != false {
			t.Errorf("%s: test case Expect Allowed as false but got true.", testCase.Name)
		}

		if testCase.ExpectErr == true && !strings.Contains(ret, testCase.ret) {
			t.Errorf("%s: test case Expect error msg :%s, but got diff error %v", testCase.Name, testCase.ret, ret)
		}

		if testCase.ExpectErr == false && ret != "" {
			t.Errorf("%s: test case Expect no error, but got error %v", testCase.Name, ret)
		}

		if testCase.ExpectErr == false && testCase.reviewResponse.Allowed != true {
			t.Errorf("%s: test case Expect Allowed as true but got false. %v", testCase.Name, testCase.reviewResponse)
		}
	}
}
