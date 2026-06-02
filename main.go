package main

import (
	"fmt"

	"github.com/libops/sitectl-drupal/cmd"
	"github.com/libops/sitectl/pkg/plugin"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	sdk := plugin.NewSDK(plugin.Metadata{
		Name:         "drupal",
		Version:      fmt.Sprintf("%s (Built on %s from Git SHA %s)", version, date, commit),
		Description:  "Drupal utilities and migration tools",
		Author:       "libops",
		TemplateRepo: "https://github.com/libops/drupal",
	})

	cmd.RegisterCommands(sdk)

	sdk.Execute()
}
