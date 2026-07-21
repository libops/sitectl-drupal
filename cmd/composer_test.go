package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
)

func TestComposerExecOptionsUsesStructuredArgsAndDrupalRoot(t *testing.T) {
	args := []string{
		"require",
		"drupal/islandora:^2.11",
		"--with-all-dependencies",
		`--repository={"type":"path","url":"../local module"}`,
	}
	ctx := &config.Context{DrupalContainerRoot: " /srv/drupal "}

	got, err := composerExecOptions(ctx, "example-drupal-1", args)
	if err != nil {
		t.Fatalf("composerExecOptions() error = %v", err)
	}

	wantArgs := append([]string{"composer"}, args...)
	if !reflect.DeepEqual(got.Cmd, wantArgs) {
		t.Fatalf("command args = %#v, want %#v", got.Cmd, wantArgs)
	}
	if got.Container != "example-drupal-1" {
		t.Fatalf("container = %q, want %q", got.Container, "example-drupal-1")
	}
	if got.WorkingDir != "/srv/drupal" {
		t.Fatalf("working directory = %q, want %q", got.WorkingDir, "/srv/drupal")
	}
	if !got.AttachStdin || !got.AttachStdout || !got.AttachStderr || !got.Tty {
		t.Fatalf("expected interactive streams and TTY to be attached: %+v", got)
	}
}

func TestComposerExecOptionsRequiresRunningContainer(t *testing.T) {
	_, err := composerExecOptions(&config.Context{}, "  ", []string{"install"})
	if err == nil {
		t.Fatal("expected composerExecOptions() error")
	}
	if !strings.Contains(err.Error(), "no running Drupal container found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
