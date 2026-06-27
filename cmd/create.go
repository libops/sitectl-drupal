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
			"mkdir -p ./certs",
			"id -u > ./certs/UID",
			"docker compose pull --ignore-buildable --ignore-pull-failures",
			"docker compose build --pull",
		},
		DockerComposeInit: []string{
			"if [ ! -f .env ]; then cp sample.env .env; fi",
			"mkdir -p ./certs",
			"id -u > ./certs/UID",
			"docker compose run --rm -e HOST_UID=\"$(id -u)\" -e HOST_GID=\"$(id -g)\" init",
		},
		DockerComposeUp:      []string{"docker compose up --remove-orphans -d"},
		DockerComposeDown:    []string{"docker compose down"},
		DockerComposeRollout: []string{"./scripts/rollout.sh"},
	}
}
