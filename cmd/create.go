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
			{Service: "drupal", Image: "libops/drupal:php84", BuildPolicy: plugin.BuildPolicyIfNotPresent},
		},
		DockerComposeInit: []string{
			"if [ ! -f .env ]; then cp sample.env .env; fi",
			"mkdir -p ./certs",
			"id -u > ./certs/UID",
			"docker compose run --rm -e HOST_UID=\"$(id -u)\" -e HOST_GID=\"$(id -g)\" init",
		},
		InitArtifacts: []plugin.InitArtifact{
			{Path: ".env"},
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
		DockerComposeUp:   []string{"docker compose up --remove-orphans -d"},
		DockerComposeDown: []string{"docker compose down"},
		DockerComposeRollout: []string{
			"docker compose pull --ignore-buildable --quiet || docker compose pull --ignore-buildable || true",
			"docker compose build --pull",
			"docker compose up --remove-orphans --wait --pull missing --quiet-pull -d",
			"docker compose exec -T drupal drush updb -y || echo \"Drupal database update skipped or failed\"",
			"docker compose exec -T drupal drush cr || echo \"Drupal cache rebuild skipped or failed\"",
			"docker compose up --remove-orphans --wait --pull missing --quiet-pull -d",
		},
	}
}
