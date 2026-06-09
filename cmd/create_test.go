package cmd

import (
	"testing"

	"github.com/libops/sitectl/pkg/plugin"
)

func TestCreateDefinition(t *testing.T) {
	spec := createDefinition()
	if spec.Name != "default" {
		t.Fatalf("expected default definition name, got %q", spec.Name)
	}
	if spec.DockerComposeRepo != drupalCreateRepo {
		t.Fatalf("expected repo %q, got %q", drupalCreateRepo, spec.DockerComposeRepo)
	}
	if spec.DockerComposeBranch != drupalCreateBranch {
		t.Fatalf("expected branch %q, got %q", drupalCreateBranch, spec.DockerComposeBranch)
	}
	if !spec.Default {
		t.Fatal("expected Drupal create definition to be the default")
	}
	if len(spec.DockerComposeInit) == 0 || spec.DockerComposeInit[0] != "make init" {
		t.Fatalf("expected make init create command, got %+v", spec.DockerComposeInit)
	}
	if len(spec.DockerComposeUp) == 0 || spec.DockerComposeUp[0] != "make up" {
		t.Fatalf("expected make up create command, got %+v", spec.DockerComposeUp)
	}
	if len(spec.DockerComposeRollout) == 0 || spec.DockerComposeRollout[0] != "make rollout" {
		t.Fatalf("expected make rollout command, got %+v", spec.DockerComposeRollout)
	}
}

func TestRegisterCommandsKeepsCoreLifecycleCommandsOutOfPlugin(t *testing.T) {
	sdk := plugin.NewSDK(plugin.Metadata{Name: "drupal"})
	if err := RegisterCommands(sdk); err != nil {
		t.Fatalf("RegisterCommands() error = %v", err)
	}

	for _, name := range []string{"build", "init", "up", "down", "status", "logs", "rollout"} {
		if hasRootCommand(sdk, name) {
			t.Fatalf("did not expect core lifecycle command %q to be registered by the plugin", name)
		}
	}

	for _, name := range []string{"drush", "uli", "sync"} {
		if !hasRootCommand(sdk, name) {
			t.Fatalf("expected plugin command %q to be registered", name)
		}
	}

}

func hasRootCommand(sdk *plugin.SDK, name string) bool {
	for _, cmd := range sdk.RootCmd.Commands() {
		if cmd.Name() == name {
			return true
		}
	}
	return false
}
