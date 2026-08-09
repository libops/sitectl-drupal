package cmd

import (
	"os"
	"strings"
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
	if len(spec.DockerComposeInit) != 4 || spec.DockerComposeInit[0] != "mkdir -p ./certs" {
		t.Fatalf("expected init create commands, got %+v", spec.DockerComposeInit)
	}
	if spec.DockerComposeInit[2] != "docker compose run --rm -e HOST_UID=\"$(id -u)\" -e HOST_GID=\"$(id -g)\" init" {
		t.Fatalf("expected init service command, got %+v", spec.DockerComposeInit)
	}
	if spec.DockerComposeInit[3] != drupalRolloutPreflightCommand {
		t.Fatalf("expected the checked-in template preflight after initialization, got %+v", spec.DockerComposeInit)
	}
	var foundUID bool
	for _, artifact := range spec.InitArtifacts {
		if artifact.Path == "certs/UID" && artifact.ValueFrom == plugin.InitArtifactValueFromHostUID {
			foundUID = true
		}
	}
	if !foundUID {
		t.Fatalf("expected host UID init artifact, got %+v", spec.InitArtifacts)
	}
	if len(spec.Images) != 1 || spec.Images[0].Service != "drupal" || spec.Images[0].Image != "libops/drupal:php84" {
		t.Fatalf("expected Drupal image spec, got %+v", spec.Images)
	}
	if spec.Images[0].BuildPolicy != plugin.BuildPolicyAlways {
		t.Fatalf("expected downstream Drupal image to always build, got %q", spec.Images[0].BuildPolicy)
	}
	if len(spec.DockerComposeUp) != 1 || !strings.Contains(spec.DockerComposeUp[0], "--wait --wait-timeout 600") {
		t.Fatalf("create must wait for service health before reporting ready: %+v", spec.DockerComposeUp)
	}
	if len(spec.DockerComposeRollout) != 6 {
		t.Fatalf("unexpected rollout commands: %+v", spec.DockerComposeRollout)
	}
	wantInitialStart := "docker compose up --remove-orphans --pull missing --quiet-pull -d drupal"
	if spec.DockerComposeRollout[2] != wantInitialStart || strings.Contains(spec.DockerComposeRollout[2], "--wait") {
		t.Fatalf("initial rollout start must target only Drupal without waiting: %q", spec.DockerComposeRollout[2])
	}
	if spec.DockerComposeRollout[3] != "docker compose exec -T drupal "+drupalWaitInstalledTarget {
		t.Fatalf("rollout must invoke the checked-in bounded readiness program: %+v", spec.DockerComposeRollout)
	}
	if spec.DockerComposeRollout[4] != "docker compose exec -T drupal "+drupalMigrationTarget || strings.Contains(spec.DockerComposeRollout[4], "||") {
		t.Fatalf("rollout must invoke the checked-in fail-hard migration program: %+v", spec.DockerComposeRollout)
	}
	wantFinalStart := "docker compose up --remove-orphans --wait --wait-timeout 600 --pull missing --quiet-pull -d"
	if spec.DockerComposeRollout[5] != wantFinalStart || strings.Contains(spec.DockerComposeRollout[5], "||") {
		t.Fatalf("final rollout start must wait for the full stack and fail hard: %q", spec.DockerComposeRollout[5])
	}
}

func TestCreateAndVerifySourcesDoNotEmbedContainerPrograms(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"create.go", "verify.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", name, err)
		}
		for _, forbidden := range []string{"php:eval", "until test -f /installed", "sh -c '"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("%s embeds forbidden container program fragment %q", name, forbidden)
			}
		}
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

	for _, name := range []string{"composer", "drush", "uli", "sync"} {
		if !hasRootCommand(sdk, name) {
			t.Fatalf("expected plugin command %q to be registered", name)
		}
	}
	if len(sdk.DeployDefinitions()) != 1 || sdk.DeployDefinitions()[0].Name != "default" {
		t.Fatalf("expected the Drupal template compatibility deploy hook, got %+v", sdk.DeployDefinitions())
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
