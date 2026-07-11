package cmd

import "github.com/libops/sitectl/pkg/plugin"

const (
	drupalCreateRepo       = "https://github.com/libops/drupal"
	drupalCreateBranch     = "main"
	drupalCreateDrupalRoot = "."
	drupalContainerRoot    = "/var/www/drupal"
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
			"docker compose exec -T drupal sh -c 'attempt=0; until test -f /installed; do attempt=$((attempt + 1)); if [ \"$attempt\" -ge 150 ]; then echo \"Drupal did not become ready for database migration within 5 minutes\" >&2; exit 1; fi; sleep 2; done'",
			"docker compose exec -T drupal drush updb -y",
			"docker compose exec -T drupal drush cr",
			"docker compose up --remove-orphans --wait --wait-timeout 600 --pull missing --quiet-pull -d",
		},
	}
}
