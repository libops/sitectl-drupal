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
	if len(spec.DockerComposeInit) != 4 || spec.DockerComposeInit[0] != "if [ ! -f .env ]; then cp sample.env .env; fi" {
		t.Fatalf("expected inline init create commands, got %+v", spec.DockerComposeInit)
	}
	if spec.DockerComposeInit[3] != "docker compose run --rm -e HOST_UID=\"$(id -u)\" -e HOST_GID=\"$(id -g)\" init" {
		t.Fatalf("expected init service command, got %+v", spec.DockerComposeInit)
	}
	if len(spec.DockerComposeUp) == 0 || spec.DockerComposeUp[0] != "docker compose up --remove-orphans -d" {
		t.Fatalf("expected compose up create command, got %+v", spec.DockerComposeUp)
	}
	if len(spec.DockerComposeRollout) == 0 || spec.DockerComposeRollout[0] != "./scripts/rollout.sh" {
		t.Fatalf("expected rollout script command, got %+v", spec.DockerComposeRollout)
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
