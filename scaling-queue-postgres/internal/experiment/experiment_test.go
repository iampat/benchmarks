package experiment_test

import (
	"strings"
	"testing"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/experiment"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

// The step list and the stage list must agree. A stage added or removed in one
// place and not the other fails here, which is the point of the package.
func TestStepsMatchStages(t *testing.T) {
	steps := experiment.Steps()
	if len(steps) != 10 {
		t.Fatalf("got %d steps, want 10", len(steps))
	}

	stages := queue.Stages()
	for i, st := range steps {
		if st.N != i {
			t.Errorf("step at index %d numbers itself %d", i, st.N)
		}
		if _, ok := queue.StageByName(st.Stage); !ok {
			t.Errorf("step %d names stage %q, which does not exist", st.N, st.Stage)
		}
		if !strings.HasPrefix(st.Title, string(rune('0'+st.N))+".") {
			t.Errorf("step %d title %q does not lead with its number", st.N, st.Title)
		}
	}

	// Steps 0 to 6 cover every stage, in order.
	if len(stages) != 7 {
		t.Fatalf("got %d stages, want 7", len(stages))
	}
	for i, stage := range stages {
		if steps[i].Stage != stage.Name {
			t.Errorf("step %d uses stage %q, want %q", i, steps[i].Stage, stage.Name)
		}
	}
	// Steps 7 to 9 keep the last stage and change only the machine.
	last := stages[len(stages)-1].Name
	for _, n := range []int{7, 8, 9} {
		if steps[n].Stage != last {
			t.Errorf("step %d uses stage %q, want the last stage %q", n, steps[n].Stage, last)
		}
	}
}

func TestStepMachines(t *testing.T) {
	want := []int{2, 2, 2, 2, 2, 2, 2, 4, 8, 0}
	for i, st := range experiment.Steps() {
		if st.Env.VMCPUs != want[i] {
			t.Errorf("step %d runs on %d CPUs, want %d", st.N, st.Env.VMCPUs, want[i])
		}
	}
	if !experiment.Steps()[9].Env.Metal() {
		t.Error("step 9 must be metal")
	}
}

func TestVerifyEnvRefusesTheWrongMachine(t *testing.T) {
	vm8, _ := experiment.ByN(8)
	if err := vm8.VerifyEnv(8, true); err != nil {
		t.Errorf("8 CPUs on a container is step 8: %v", err)
	}
	if err := vm8.VerifyEnv(2, true); err == nil {
		t.Error("step 8 accepted a machine reporting 2 CPUs")
	}
	if err := vm8.VerifyEnv(0, false); err == nil {
		t.Error("step 8 accepted a run with no container")
	}

	bare, _ := experiment.ByN(9)
	if err := bare.VerifyEnv(0, false); err != nil {
		t.Errorf("no container is step 9: %v", err)
	}
	if err := bare.VerifyEnv(8, true); err == nil {
		t.Error("step 9 accepted a managed container")
	}
}

func TestByN(t *testing.T) {
	if _, ok := experiment.ByN(10); ok {
		t.Error("ByN(10) = ok, want miss")
	}
	s, ok := experiment.ByN(6)
	if !ok || s.Stage != "6-sharded" {
		t.Errorf("ByN(6) = %+v, %v", s, ok)
	}
}
