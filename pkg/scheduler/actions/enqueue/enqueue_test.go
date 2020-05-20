package enqueue

import (
	"testing"

	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestGetOverCommitFactor(t *testing.T) {
	cases := []struct {
		name          string
		ssn           *framework.Session
		expectedValue float64
	}{
		{
			name: "arguments of action not exist",
			ssn: &framework.Session{
				Configurations: []conf.Configuration{
					{
						Name: "allocate",
						Arguments: map[string]string{
							"placeholder": "placeholder",
						},
					},
				},
			},
			expectedValue: 1.2,
		},
		{
			name: "arguments of action exist",
			ssn: &framework.Session{
				Configurations: []conf.Configuration{
					{
						Name: "enqueue",
						Arguments: map[string]string{
							overCommitFactor: "2",
						},
					},
				},
			},
			expectedValue: 2,
		},
	}

	enqueue := New()
	for index, c := range cases {
		factor := enqueue.getOverCommitFactor(c.ssn)
		if factor != c.expectedValue {
			t.Errorf("index %d, case %s, expected %v, but got %v", index, c.name, c.expectedValue, factor)
		}
	}
}
