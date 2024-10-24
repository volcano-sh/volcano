package util

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"os"
	"path"
	"testing"
)

func TestGenerateSchedulerName(t *testing.T) {
	tmpDir := t.TempDir()
	fmt.Println("tmpDir:", tmpDir)
	dir := path.Join(tmpDir, "cpu", "kubepods", "pod", "infodir")
	err := os.MkdirAll(dir, 0644)
	assert.NoError(t, err)
	tests := []struct {
		name           string
		schedulerNames []string
		want           string
	}{
		{
			name: "test",
			want: "volcano",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateSchedulerName(tt.schedulerNames); got != tt.want {
				t.Errorf("GenerateSchedulerName() = %v, want %v", got, tt.want)
			}
		})
	}
}
