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

func TestRegisterCommandsAddsStandardComposeCommands(t *testing.T) {
	sdk := plugin.NewSDK(plugin.Metadata{Name: "drupal"})
	RegisterCommands(sdk)

	for _, name := range []string{"build", "init", "up", "down", "status", "logs", "rollout"} {
		if _, _, err := sdk.RootCmd.Find([]string{name}); err != nil {
			t.Fatalf("expected %q command to be registered: %v", name, err)
		}
	}
}
