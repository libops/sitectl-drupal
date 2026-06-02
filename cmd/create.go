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
		Name:                 "default",
		Description:          "Create a Docker Compose Drupal stack",
		Default:              true,
		MinCPUCores:          2,
		MinMemory:            "4 GiB",
		MinDiskSpace:         "20 GiB",
		DockerComposeRepo:    drupalCreateRepo,
		DockerComposeBranch:  drupalCreateBranch,
		DockerComposeBuild:   []string{"make build"},
		DockerComposeInit:    []string{"make init"},
		DockerComposeUp:      []string{"make up"},
		DockerComposeDown:    []string{"make down"},
		DockerComposeRollout: []string{"make rollout"},
	}
}
