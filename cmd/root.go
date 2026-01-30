package cmd

import (
	"github.com/libops/sitectl/pkg/plugin"
)

var (
	drupalServiceName = func() *string { s := "drupal"; return &s }()
	sdk               *plugin.SDK
)

func init() {
	loginCmd.Flags().Uint("uid", 1, "Drupal user ID to provide a direct login link for")

	backupCmd.Flags().StringVarP(drupalServiceName, "drupal-service", "d", "drupal", "The name of the drupal service in docker compose")
}

// RegisterCommands registers all drupal commands with the plugin SDK
func RegisterCommands(s *plugin.SDK) {
	sdk = s
	sdk.AddCommand(backupCmd)
	sdk.AddCommand(drushCmd)
	sdk.AddCommand(loginCmd)
	sdk.AddCommand(nodeCmd)
}
