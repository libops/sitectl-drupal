package cmd

import (
	pluginjobs "github.com/libops/sitectl-drupal/pkg/jobs"
	"github.com/libops/sitectl/pkg/plugin"
)

var (
	drupalServiceName = func() *string { s := "drupal"; return &s }()
	sdk               *plugin.SDK
)

func init() {
	loginCmd.Flags().Uint("uid", 1, "Drupal user ID to generate the login link for.")
}

// RegisterCommands registers all drupal commands with the plugin SDK
func RegisterCommands(s *plugin.SDK) {
	sdk = s
	pluginjobs.Register(s)
	sdk.AddCommand(sdk.GetMetadataCommand())
	sdk.AddCommand(componentExtensionCmd)
	sdk.AddCommand(debugExtensionCmd)
	sdk.AddCommand(drushCmd)
	sdk.AddCommand(loginCmd)
	sdk.AddCommand(syncCmd)
}
