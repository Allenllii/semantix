package boot

import (
	"testing"

	"semantix/harness/testenv"
)

func TestMain(m *testing.M) {
	testenv.RunWithIsolatedUserState(m)
}
