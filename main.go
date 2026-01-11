package main

import (
	"github.com/libops/sitectl-drupal/cmd"
	"github.com/libops/sitectl/pkg/plugin"
)

func main() {
	sdk := plugin.NewSDK(plugin.Metadata{
		Name:        "drupal",
		Version:     "v0.0.3",
		Description: "Drupal utilities and migration tools",
		Author:      "libops",
	})

	cmd.RegisterCommands(sdk)

	sdk.Execute()
}
