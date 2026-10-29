package game

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Off Windows cargo builds Benilla without ".exe": Meld finds that program, and its launcher first.
func TestBenillaBuiltOffWindows(t *testing.T) {
	onHost(t, "darwin")
	folder := t.TempDir()
	release := filepath.Join(folder, "target", "release")
	if err := os.MkdirAll(filepath.Join(release, "benilla.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if BenillaBuilt(folder) || BenillaExe(folder) != filepath.Join(release, "benilla") {
		t.Fatalf("nothing built: %v, %s", BenillaBuilt(folder), BenillaExe(folder))
	}
	for _, name := range []string{"benilla", "meld-benilla"} {
		if err := os.WriteFile(filepath.Join(release, name), nil, 0o755); err != nil {
			t.Fatal(err)
		}
		if !BenillaBuilt(folder) || BenillaExe(folder) != filepath.Join(release, name) {
			t.Fatalf("with %s: %v, %s", name, BenillaBuilt(folder), BenillaExe(folder))
		}
	}
}

// scriptRunner answers Run with fixed output lines and error.
type scriptRunner struct {
	fakeRunner
	lines []string
	err   error
	dir   string
	args  []string
}

func (r *scriptRunner) Run(dir, name string, args []string, log func(string)) error {
	r.dir, r.args = dir, args
	for _, l := range r.lines {
		log(l)
	}
	return r.err
}

func TestBuildBenillaRunsCargoInTheFolderAndNamesItsError(t *testing.T) {
	r := &scriptRunner{lines: []string{"Compiling benilla-mpq v0.1.0 (/b/crates/benilla-mpq)", "error[E0425]: cannot find value `x`", "error: could not compile `benilla-mpq`"}, err: errors.New("exit status 101")}
	var steps []string
	err := BuildBenilla(r, "cargo", "/b", func(line string) {
		if s := BuildStep(line); s != "" {
			steps = append(steps, s)
		}
	})
	if r.dir != "/b" || len(r.args) != 4 || r.args[3] != "benilla" {
		t.Fatalf("ran in %q with %v", r.dir, r.args)
	}
	if err == nil || err.Error() != "error[E0425]: cannot find value `x` (exit status 101)" {
		t.Fatalf("error %v", err)
	}
	if len(steps) != 1 || steps[0] != "benilla-mpq" {
		t.Fatalf("steps %v", steps)
	}
	r.err = nil
	if err := BuildBenilla(r, "cargo", "/b", nil); err != nil {
		t.Fatal(err)
	}
}
