package cmd

import "github.com/libops/sitectl/pkg/plugin"

const (
	drupalCreateRepo       = "https://github.com/libops/drupal"
	drupalCreateBranch     = drupalTemplateVersion
	drupalCreateDrupalRoot = "."
	drupalContainerRoot    = "/var/www/drupal"
	drushExecutable        = "/var/www/drupal/vendor/bin/drush"
)

func createDefinition() plugin.CreateSpec {
	return plugin.CreateSpec{
		Name:                "default",
		Description:         "Create a Docker Compose Drupal stack",
		Default:             true,
		MinCPUCores:         2,
		MinMemory:           "4 GiB",
		MinDiskSpace:        "20 GiB",
		DockerComposeRepo:   drupalCreateRepo,
		DockerComposeBranch: drupalCreateBranch,
		DockerComposeBuild: []string{
			"docker compose pull --ignore-buildable --ignore-pull-failures",
			"docker compose build",
		},
		Images: []plugin.ComposeImageSpec{
			{Service: "drupal", Image: "libops/drupal:php84", BuildPolicy: plugin.BuildPolicyAlways},
		},
		DockerComposeInit: []string{
			"mkdir -p ./certs",
			"id -u > ./certs/UID",
			"docker compose run --rm -e HOST_UID=\"$(id -u)\" -e HOST_GID=\"$(id -g)\" init",
			drupalRolloutPreflightCommand,
		},
		InitArtifacts: []plugin.InitArtifact{
			{Path: "certs/cert.pem"},
			{Path: "certs/privkey.pem"},
			{Path: "certs/rootCA.pem"},
			{Path: "certs/rootCA-key.pem"},
			{Path: "certs/UID", ValueFrom: plugin.InitArtifactValueFromHostUID},
			{Path: "secrets/DB_ROOT_PASSWORD"},
			{Path: "secrets/DRUPAL_DEFAULT_ACCOUNT_PASSWORD"},
			{Path: "secrets/DRUPAL_DEFAULT_DB_PASSWORD"},
			{Path: "secrets/DRUPAL_DEFAULT_SALT"},
		},
		DockerComposeUp:   []string{"docker compose up --remove-orphans --wait --wait-timeout 600 -d"},
		DockerComposeDown: []string{"docker compose down"},
		DockerComposeRollout: []string{
			"docker compose pull --ignore-buildable --quiet || docker compose pull --ignore-buildable",
			"docker compose build --pull",
			"docker compose up --remove-orphans --pull missing --quiet-pull -d drupal",
			"docker compose exec -T drupal " + drupalWaitInstalledTarget,
			"docker compose exec -T drupal " + drupalMigrationTarget,
			"docker compose up --remove-orphans --wait --wait-timeout 600 --pull missing --quiet-pull -d",
		},
	}
}
