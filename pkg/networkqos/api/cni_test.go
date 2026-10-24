package api

import (
	"encoding/json"
	"net"
	"reflect"
	"testing"

	"github.com/containernetworking/cni/pkg/types"
)

func TestNetConfJSONRoundTrip(t *testing.T) {
	original := NetConf{
		NetConf: types.NetConf{
			CNIVersion: "0.4.0",
			Name:       "volcano-net",
			Type:       "bridge",
		},
		Args: map[string]string{
			"foo": "bar",
			"baz": "qux",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal NetConf: %v", err)
	}

	var parsed NetConf
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal NetConf: %v", err)
	}

	if parsed.CNIVersion != original.CNIVersion {
		t.Errorf("CNIVersion mismatch: got %q, want %q", parsed.CNIVersion, original.CNIVersion)
	}
	if parsed.Name != original.Name {
		t.Errorf("Name mismatch: got %q, want %q", parsed.Name, original.Name)
	}
	if parsed.Type != original.Type {
		t.Errorf("Type mismatch: got %q, want %q", parsed.Type, original.Type)
	}

	if !reflect.DeepEqual(parsed.Args, original.Args) {
		t.Errorf("Args mismatch: got %#v, want %#v", parsed.Args, original.Args)
	}
}

func TestK8sArgsFieldAssignment(t *testing.T) {
	ip := net.ParseIP("10.1.2.3")
	if ip == nil {
		t.Fatal("failed to parse IP")
	}

	a := K8sArgs{}
	a.IP = ip
	a.K8S_POD_NAME = types.UnmarshallableString("mypod")
	a.K8S_POD_NAMESPACE = types.UnmarshallableString("myns")
	a.K8S_POD_INFRA_CONTAINER_ID = types.UnmarshallableString("container123")
	a.K8S_POD_UID = types.UnmarshallableString("uid-abc")
	a.K8S_POD_RUNTIME = types.UnmarshallableString("docker")
	a.SECURE_CONTAINER = types.UnmarshallableString("deprecated")

	// Verify IP
	if !a.IP.Equal(ip) {
		t.Errorf("IP mismatch: got %q, want %q", a.IP, ip)
	}

	// Verify each UnmarshallableString
	fields := map[string]types.UnmarshallableString{
		"K8S_POD_NAME":               a.K8S_POD_NAME,
		"K8S_POD_NAMESPACE":          a.K8S_POD_NAMESPACE,
		"K8S_POD_INFRA_CONTAINER_ID": a.K8S_POD_INFRA_CONTAINER_ID,
		"K8S_POD_UID":                a.K8S_POD_UID,
		"K8S_POD_RUNTIME":            a.K8S_POD_RUNTIME,
		"SECURE_CONTAINER":           a.SECURE_CONTAINER,
	}
	for name, val := range fields {
		if string(val) == "" {
			t.Errorf("%s was not set correctly", name)
		}
	}
}
