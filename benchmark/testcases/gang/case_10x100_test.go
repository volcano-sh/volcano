package gang_test

import "testing"

// TestGang10x100 tests gang scheduling with 10 jobs × 100 pods/job (1000 total pods).
func TestGang10x100(t *testing.T) {
	RunGangTest(t, RepeatConfig(VCJobConfig{
		Name:         "gang-100",
		Replicas:     100,
		MinAvailable: 100,
		CPU:          "1",
		Memory:       "1Gi",
		Queue:        "benchmark-queue",
	}, 10))
}
