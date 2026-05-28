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

package options

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckOptionOrDieTLSFlags(t *testing.T) {
	tests := []struct {
		name    string
		ca      string
		cert    string
		key     string
		wantErr bool
	}{
		{name: "none set"},
		{name: "all set", ca: "ca", cert: "cert", key: "key"},
		{name: "partial ca only", ca: "ca", wantErr: true},
		{name: "partial cert only", cert: "cert", wantErr: true},
		{name: "partial key only", key: "key", wantErr: true},
		{name: "partial ca and cert", ca: "ca", cert: "cert", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServerOption()
			s.CaCertFile = tt.ca
			s.CertFile = tt.cert
			s.KeyFile = tt.key
			err := s.CheckOptionOrDie()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
