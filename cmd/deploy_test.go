package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

func TestDrupalDeployRunnerPreDownChecksTrackedAndEffectivePrograms(t *testing.T) {
	projectDir := writeDrupalProgramContractFixture(t, drupalProgramComposeFixture(true))
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}

	oldRun := drupalRunComposeProjectCommand
	t.Cleanup(func() { drupalRunComposeProjectCommand = oldRun })
	var commands []string
	drupalRunComposeProjectCommand = func(_ context.Context, gotCtx *config.Context, gotProjectDir string, _, _ io.Writer, command string) error {
		if gotCtx != ctx || gotProjectDir != projectDir {
			t.Fatalf("unexpected rollout context: ctx=%#v project=%q", gotCtx, gotProjectDir)
		}
		commands = append(commands, command)
		return nil
	}

	cmd := &cobra.Command{Use: "pre-down"}
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := (drupalDeployRunner{}).PreDown(cmd, ctx); err != nil {
		t.Fatalf("PreDown() error = %v", err)
	}

	want := []string{drupalRolloutPreflightCommand, drupalComposeConfigCommand}
	for _, program := range drupalTemplatePrograms {
		probe := "-r"
		if program.executable {
			probe = "-x"
		}
		want = append(want, "docker compose run --rm --no-deps --entrypoint test drupal "+probe+" "+program.target)
	}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("effective Compose checks = %#v, want %#v", commands, want)
	}
}

func TestDrupalDeployRunnerPreDownFailsClearlyWhenTrackedPreflightRejects(t *testing.T) {
	projectDir := writeDrupalProgramContractFixture(t, drupalProgramComposeFixture(true))
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}

	oldRun := drupalRunComposeProjectCommand
	t.Cleanup(func() { drupalRunComposeProjectCommand = oldRun })
	drupalRunComposeProjectCommand = func(_ context.Context, _ *config.Context, _ string, _, _ io.Writer, command string) error {
		if command == drupalRolloutPreflightCommand {
			return errors.New("rejected source")
		}
		return nil
	}

	err := (drupalDeployRunner{}).PreDown(&cobra.Command{Use: "pre-down"}, ctx)
	assertDrupalTemplateMigrationError(t, err, "tracked preflight")
}

func TestValidateDrupalTemplateProgramsAcceptsReadOnlyMounts(t *testing.T) {
	t.Parallel()
	projectDir := writeDrupalProgramContractFixture(t, drupalProgramComposeFixture(true))
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	if err := validateDrupalTemplatePrograms(t.Context(), ctx); err != nil {
		t.Fatalf("validateDrupalTemplatePrograms() error = %v", err)
	}
}

func TestValidateDrupalTemplateProgramsRejectsOlderCheckout(t *testing.T) {
	t.Parallel()
	projectDir := writeDrupalProgramContractFixture(t, drupalProgramComposeFixture(true))
	if err := os.Remove(filepath.Join(projectDir, filepath.FromSlash(drupalVerifySolrSource))); err != nil {
		t.Fatalf("Remove(program) error = %v", err)
	}
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	assertDrupalTemplateMigrationError(t, validateDrupalTemplatePrograms(t.Context(), ctx), "missing tracked "+drupalVerifySolrSource)
}

func TestValidateDrupalTemplateProgramsRejectsWritableMount(t *testing.T) {
	t.Parallel()
	projectDir := writeDrupalProgramContractFixture(t, drupalProgramComposeFixture(false))
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	assertDrupalTemplateMigrationError(t, validateDrupalTemplatePrograms(t.Context(), ctx), "without the required read-only checked-in bind")
}

func TestValidateDrupalTemplateProgramsIgnoresUnconfiguredLegacyComposeFile(t *testing.T) {
	t.Parallel()
	projectDir := writeDrupalProgramContractFixture(t, drupalProgramComposeFixture(true))
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yaml"), []byte(drupalProgramComposeFixture(false)), 0o644); err != nil {
		t.Fatalf("WriteFile(docker-compose.yaml) error = %v", err)
	}
	ctx := &config.Context{
		DockerHostType: config.ContextLocal,
		ProjectDir:     projectDir,
		ComposeFile:    []string{"compose.yaml"},
	}
	if err := validateDrupalTemplatePrograms(t.Context(), ctx); err != nil {
		t.Fatalf("validateDrupalTemplatePrograms() inspected an unconfigured Compose file: %v", err)
	}
}

func TestValidateDrupalTemplateProgramsRejectsSymlink(t *testing.T) {
	t.Parallel()
	projectDir := writeDrupalProgramContractFixture(t, drupalProgramComposeFixture(true))
	programPath := filepath.Join(projectDir, filepath.FromSlash(drupalWaitInstalledSource))
	if err := os.Remove(programPath); err != nil {
		t.Fatalf("Remove(program) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(projectDir, "compose.yaml"), programPath); err != nil {
		t.Fatalf("Symlink(program) error = %v", err)
	}
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	assertDrupalTemplateMigrationError(t, validateDrupalTemplatePrograms(t.Context(), ctx), "must be a regular tracked file")
}

func writeDrupalProgramContractFixture(t *testing.T, compose string) string {
	t.Helper()
	projectDir := t.TempDir()
	for _, program := range append([]drupalTemplateProgram{{source: drupalRolloutPreflightSource, executable: true}}, drupalTemplatePrograms...) {
		path := filepath.Join(projectDir, filepath.FromSlash(program.source))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
		mode := os.FileMode(0o644)
		if program.executable {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte("fixture\n"), mode); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yaml) error = %v", err)
	}
	return projectDir
}

func drupalProgramComposeFixture(readOnly bool) string {
	mode := "ro,z"
	if !readOnly {
		mode = "rw,z"
	}
	var lines []string
	for _, program := range drupalTemplatePrograms {
		lines = append(lines, "      - ./"+program.source+":"+program.target+":"+mode)
	}
	return "services:\n  drupal:\n    volumes:\n" + strings.Join(lines, "\n") + "\n"
}

func assertDrupalTemplateMigrationError(t *testing.T, err error, detail string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected Drupal template compatibility failure")
	}
	for _, want := range []string{
		"before services were stopped",
		detail,
		drupalCreateRepo,
		drupalTemplateVersion,
		"rerun sitectl deploy",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %v", want, err)
		}
	}
}
