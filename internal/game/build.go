package game

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"Meld/internal/vmangos"
)

// ErrNoCargo is BuildBenilla's error when Rust isn't installed, or isn't where rustup puts it.
var ErrNoCargo = errors.New("building Benilla needs Rust: install it from https://rustup.rs, then build again")

// Cargo finds cargo on PATH or, for a Meld started from the desktop without the shell's PATH, where
// rustup installs it.
func Cargo() (string, error) {
	if p, err := exec.LookPath("cargo"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range imageFiles("cargo.exe") {
			p := filepath.Join(home, ".cargo", "bin", name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", ErrNoCargo
}

// BuildBenilla builds Benilla's program in its folder with `cargo build --release -p benilla`, the dev
// build that reads the client from WOW_DATA (BENILLA.md). It passes each output line to log. A failed
// build's error names cargo's first error line.
func BuildBenilla(run vmangos.Runner, cargo, folder string, log func(string)) error {
	first := ""
	err := run.Run(folder, cargo, []string{"build", "--release", "-p", "benilla"}, func(line string) {
		if first == "" && strings.HasPrefix(line, "error") {
			first = line
		}
		if log != nil {
			log(line)
		}
	})
	if err == nil {
		return nil
	}
	if first != "" {
		return fmt.Errorf("%s (%w)", first, err)
	}
	return err
}

// BuildStep is the crate a cargo output line says it is compiling, or "" for any other line.
func BuildStep(line string) string {
	rest, ok := strings.CutPrefix(line, "Compiling ")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, " ")
	return name
}
