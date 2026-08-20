// Package experiment holds the agreed shape of the run. The step list here is
// the only place that says what a step is, which queue stage it uses, and which
// machine it belongs on. The report, the runner, and the tests all read it.
package experiment

import "fmt"

type Env struct {
	Name string
	// 0 means the host, with no container.
	VMCPUs int
}

func (e Env) Metal() bool { return e.VMCPUs == 0 }

type Step struct {
	N      int
	Title  string
	Label  string
	Change string
	Stage  string
	Env    Env
	Source string
}

var (
	vm2   = Env{Name: "VM, 2 CPUs", VMCPUs: 2}
	vm4   = Env{Name: "VM, 4 CPUs", VMCPUs: 4}
	vm8   = Env{Name: "VM, 8 CPUs", VMCPUs: 8}
	metal = Env{Name: "metal"}
)

// Steps 0 to 6 change the queue and share one machine. Steps 7 to 9 keep the
// last queue and change only the machine, so the design and the hardware are
// measured apart.
func Steps() []Step {
	return []Step{
		{
			0, "0. Naive claim query", "0 vanilla",
			"`FOR UPDATE`, `REPEATABLE READ`, btree on `(queue_name, created_at)`",
			"0-vanilla", vm2, "article",
		},
		{
			1, "1. `SKIP LOCKED`", "1 SKIP LOCKED",
			"`FOR UPDATE SKIP LOCKED`", "1-skip-locked", vm2, "article",
		},
		{
			2, "2. `READ COMMITTED`", "2 READ COMMITTED",
			"The claim runs at `READ COMMITTED`", "2-read-committed", vm2, "article",
		},
		{
			3, "3. Partial covering index", "3 partial index",
			"`(queue_name, status, priority, created_at) WHERE status = 'CREATED'`",
			"3-partial-index", vm2, "article",
		},
		{
			4, "4. `synchronous_commit = off`", "4 async commit",
			"The commit returns before the flush", "4-async-commit", vm2, "ours",
		},
		{
			5, "5. One statement per claim", "5 one statement",
			"One CTE replaces a transaction of two statements", "5-single-statement", vm2, "ours",
		},
		{
			6, "6. Shard the queue head", "6 sharded",
			"A `shard` column, one pinned worker set per shard", "6-sharded", vm2, "ours",
		},
		{
			7, "7. Twice the CPUs", "7 VM, 4 CPUs",
			"The virtual machine goes from 2 CPUs to 4", "6-sharded", vm4, "ours",
		},
		{
			8, "8. Twice the CPUs again", "8 VM, 8 CPUs",
			"The virtual machine goes from 4 CPUs to 8", "6-sharded", vm8, "ours",
		},
		{
			9, "9. Leave the virtual machine", "9 metal",
			"Postgres runs on the host, with no container", "6-sharded", metal, "ours",
		},
	}
}

func ByN(n int) (Step, bool) {
	for _, s := range Steps() {
		if s.N == n {
			return s, true
		}
	}
	return Step{}, false
}

// VerifyEnv refuses a cell that did not run on the machine its step names. A
// failed reconfiguration must not produce a cell labelled for a machine it never
// saw.
func (s Step) VerifyEnv(observedVMCPUs int, managedContainer bool) error {
	if s.Env.Metal() {
		if managedContainer {
			return fmt.Errorf("step %d runs on %s, so it needs an existing server through -dsn",
				s.N, s.Env.Name)
		}
		return nil
	}
	if !managedContainer {
		return fmt.Errorf("step %d runs on %s, so it needs a managed container", s.N, s.Env.Name)
	}
	if observedVMCPUs != s.Env.VMCPUs {
		return fmt.Errorf("step %d runs on %s, but the machine reports %d CPUs",
			s.N, s.Env.Name, observedVMCPUs)
	}
	return nil
}
