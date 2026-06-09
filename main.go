package main

import (
	"fmt"
	"os"

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

	if err := cmd.RegisterCommands(sdk); err != nil {
		fmt.Fprintf(os.Stderr, "sitectl-drupal: %v\n", err)
		os.Exit(1)
	}

	sdk.Execute()
}
