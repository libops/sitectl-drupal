package main

import (
	"github.com/libops/sitectl-drupal/cmd"
	"github.com/libops/sitectl/pkg/plugin"
)

func main() {
	sdk := plugin.NewSDK(plugin.Metadata{
		Name:        "drupal",
		Version:     "1.0.0",
		Description: "Drupal utilities and migration tools",
		Author:      "libops",
	})

	cmd.RegisterCommands(sdk)

	sdk.Execute()
}
