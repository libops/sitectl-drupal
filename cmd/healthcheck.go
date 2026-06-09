package cmd

import (
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/healthcheck"
	"github.com/libops/sitectl/pkg/plugin"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
)

type drupalHealthcheckRunner struct{}

func (drupalHealthcheckRunner) BindFlags(cmd *cobra.Command) {}

func (drupalHealthcheckRunner) Run(cmd *cobra.Command, ctx *config.Context) ([]sitevalidate.Result, error) {
	results := []sitevalidate.Result{
		healthcheck.CheckHTTP(cmd.Context(), "http:drupal", healthcheck.PublicURLFromEnv(ctx, "http", "drupal.traefik.me")),
	}

	checker, err := healthcheck.NewDockerChecker(ctx)
	if err != nil {
		return nil, err
	}
	defer checker.Close()

	results = append(results,
		checker.CheckMariaDB(cmd.Context(), "mariadb"),
		checker.CheckSolrCore(cmd.Context(), "solr", "default"),
	)
	return results, nil
}

var _ plugin.HealthcheckRunner = drupalHealthcheckRunner{}
