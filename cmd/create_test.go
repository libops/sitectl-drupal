package cmd

import (
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
	if len(spec.DockerComposeInit) != 3 || spec.DockerComposeInit[0] != "mkdir -p ./certs" {
		t.Fatalf("expected inline init create commands, got %+v", spec.DockerComposeInit)
	}
	if spec.DockerComposeInit[2] != "docker compose run --rm -e HOST_UID=\"$(id -u)\" -e HOST_GID=\"$(id -g)\" init" {
		t.Fatalf("expected init service command, got %+v", spec.DockerComposeInit)
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
	if len(spec.DockerComposeRollout) == 0 || spec.DockerComposeRollout[0] == "./scripts/rollout.sh" {
		t.Fatalf("expected inline rollout commands, got %+v", spec.DockerComposeRollout)
	}
	foundMigration := false
	for index, command := range spec.DockerComposeRollout {
		if !strings.Contains(command, "drush updb") {
			continue
		}
		foundMigration = true
		if strings.Contains(command, "||") || index < 2 || !strings.Contains(spec.DockerComposeRollout[index-1], "test -f /installed") || !strings.Contains(spec.DockerComposeRollout[index-1], "-ge 150") {
			t.Fatalf("Drupal migration must fail hard after bounded readiness: %+v", spec.DockerComposeRollout)
		}
		initialStart := spec.DockerComposeRollout[index-2]
		wantInitialStart := "docker compose up --remove-orphans --pull missing --quiet-pull -d drupal"
		if initialStart != wantInitialStart ||
			!strings.HasSuffix(initialStart, " -d drupal") ||
			strings.Contains(initialStart, "--wait") {
			t.Fatalf("initial rollout start must target only Drupal without waiting: %q", initialStart)
		}
		if index+2 >= len(spec.DockerComposeRollout) {
			t.Fatalf("cache rebuild and final health wait must follow migration: %+v", spec.DockerComposeRollout)
		}
		cacheRebuild := spec.DockerComposeRollout[index+1]
		if !strings.Contains(cacheRebuild, "drush cr") || strings.Contains(cacheRebuild, "||") {
			t.Fatalf("Drupal cache rebuild must fail the rollout when it fails: %+v", spec.DockerComposeRollout)
		}
		finalStart := spec.DockerComposeRollout[index+2]
		wantFinalStart := "docker compose up --remove-orphans --wait --wait-timeout 600 --pull missing --quiet-pull -d"
		if finalStart != wantFinalStart ||
			!strings.Contains(finalStart, "--wait --wait-timeout 600") ||
			!strings.HasSuffix(finalStart, " -d") ||
			strings.Contains(finalStart, "||") {
			t.Fatalf("final rollout start must wait for the full stack and fail hard: %q", finalStart)
		}
	}
	if !foundMigration {
		t.Fatalf("Drupal rollout must run the database migration: %+v", spec.DockerComposeRollout)
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

}

func hasRootCommand(sdk *plugin.SDK, name string) bool {
	for _, cmd := range sdk.RootCmd.Commands() {
		if cmd.Name() == name {
			return true
		}
	}
	return false
}
