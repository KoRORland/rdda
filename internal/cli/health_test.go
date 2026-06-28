package cli

import "testing"

func TestHealthRequiresTarget(t *testing.T) {
	t.Setenv("RDDA_HEALTH_TO", "")
	t.Setenv("RDDA_PULL_TOKEN", "")
	root := newRoot()
	root.SetArgs([]string{"health"})
	if err := root.Execute(); err == nil {
		t.Fatal("health without a --to / $RDDA_HEALTH_TO must error")
	}
}
