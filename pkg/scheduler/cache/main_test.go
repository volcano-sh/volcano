package cache

import (
	"os"
	"testing"

	"volcano.sh/volcano/cmd/scheduler/app/options"
)

func TestMain(m *testing.M) {
	options.Default()
	os.Exit(m.Run())
}
