package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/libops/sitectl/pkg/plugin"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
)

const drupalQueueProbe = `$cron = \Drupal::service('cron'); $workers = \Drupal::service('plugin.manager.queue_worker')->getDefinitions(); print json_encode(['cron' => $cron !== NULL, 'queue_workers' => count($workers)]);`

type drupalVerifyRuntime interface {
	ExecCapture(context.Context, string, string, []string) (string, error)
}

type dockerDrupalVerifyRuntime struct {
	client *docker.DockerClient
}

func (r dockerDrupalVerifyRuntime) ExecCapture(ctx context.Context, container, workingDir string, argv []string) (string, error) {
	return docker.ExecCapture(ctx, r.client, container, workingDir, argv)
}

type drupalVerifyRunner struct{}

func (r *drupalVerifyRunner) BindFlags(_ *cobra.Command) {}

func (r *drupalVerifyRunner) Run(cmd *cobra.Command, _ *config.Context) ([]sitevalidate.Result, error) {
	ctx, cli, container, err := getDrupalContainerForSDK(cmd.Context())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	return runDrupalVerifyChecks(cmd.Context(), dockerDrupalVerifyRuntime{client: cli}, container, ctx.EffectiveDrupalContainerRoot()), nil
}

var _ plugin.VerifyRunner = (*drupalVerifyRunner)(nil)

func runDrupalVerifyChecks(ctx context.Context, runtime drupalVerifyRuntime, container, workingDir string) []sitevalidate.Result {
	type check struct {
		name string
		argv []string
		read func(string) sitevalidate.Result
		fix  string
	}

	checks := []check{
		{
			name: "verify:drupal:bootstrap",
			argv: []string{"drush", "status", "--field=bootstrap"},
			read: func(output string) sitevalidate.Result {
				if strings.EqualFold(strings.TrimSpace(output), "Successful") {
					return verifyOK("verify:drupal:bootstrap", "Drupal bootstrapped successfully")
				}
				return verifyFailed("verify:drupal:bootstrap", fmt.Sprintf("unexpected bootstrap status %q", strings.TrimSpace(output)), "inspect Drupal logs and settings.php")
			},
			fix: "inspect Drupal logs and settings.php",
		},
		{
			name: "verify:drupal:database-identity",
			argv: []string{"drush", "sql:query", "SELECT CURRENT_USER();", "--extra=--batch", "--extra=--skip-column-names"},
			read: verifyDrupalDatabaseIdentity,
			fix:  "mount only the scoped Drupal database password and recreate the application database user",
		},
		{
			name: "verify:drupal:config-drift",
			argv: []string{"drush", "config:status", "--format=json"},
			read: verifyDrupalConfigDrift,
			fix:  "export intentional configuration changes or import the committed configuration",
		},
		{
			name: "verify:drupal:cron-queue",
			argv: []string{"drush", "php:eval", drupalQueueProbe},
			read: verifyDrupalCronQueue,
			fix:  "repair Drupal cron and queue worker service discovery",
		},
		{
			name: "verify:drupal:solr",
			argv: []string{"drush", "search-api:server-status", "default_solr_server", "--format=json"},
			read: verifyDrupalSolr,
			fix:  "check the default_solr_server configuration and Solr connectivity",
		},
	}

	results := make([]sitevalidate.Result, 0, len(checks))
	for _, check := range checks {
		output, err := runtime.ExecCapture(ctx, container, workingDir, check.argv)
		if err != nil {
			results = append(results, verifyFailed(check.name, err.Error(), check.fix))
			continue
		}
		results = append(results, check.read(output))
	}
	return results
}

func verifyDrupalDatabaseIdentity(output string) sitevalidate.Result {
	identity := strings.TrimSpace(output)
	if identity == "" {
		return verifyFailed("verify:drupal:database-identity", "database returned an empty current user", "check the scoped Drupal database secret")
	}
	user, _, _ := strings.Cut(identity, "@")
	if strings.EqualFold(strings.TrimSpace(user), "root") {
		return verifyFailed("verify:drupal:database-identity", "Drupal is connected as the MariaDB root user", "configure Drupal with its scoped application database user")
	}
	return verifyOK("verify:drupal:database-identity", identity)
}

func verifyDrupalConfigDrift(output string) sitevalidate.Result {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return verifyFailed("verify:drupal:config-drift", "config:status returned no JSON", "confirm Drush can read the active and sync configuration")
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return verifyFailed("verify:drupal:config-drift", fmt.Sprintf("invalid config:status JSON: %v", err), "run drush config:status and resolve the error")
	}
	clean := false
	switch typed := value.(type) {
	case []any:
		clean = len(typed) == 0
	case map[string]any:
		clean = len(typed) == 0
	}
	if !clean {
		return verifyFailed("verify:drupal:config-drift", "active Drupal configuration differs from the sync directory", "export intentional changes or import the committed configuration")
	}
	return verifyOK("verify:drupal:config-drift", "active configuration matches the sync directory")
}

func verifyDrupalCronQueue(output string) sitevalidate.Result {
	var probe struct {
		Cron         bool `json:"cron"`
		QueueWorkers *int `json:"queue_workers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &probe); err != nil {
		return verifyFailed("verify:drupal:cron-queue", fmt.Sprintf("invalid cron/queue probe JSON: %v", err), "repair Drupal cron and queue worker service discovery")
	}
	if probe.QueueWorkers == nil {
		return verifyFailed("verify:drupal:cron-queue", "queue worker discovery result is missing", "repair Drupal queue worker service discovery")
	}
	if !probe.Cron || *probe.QueueWorkers < 0 {
		return verifyFailed("verify:drupal:cron-queue", fmt.Sprintf("cron=%t queue_workers=%d", probe.Cron, *probe.QueueWorkers), "repair Drupal cron and queue worker service discovery")
	}
	return verifyOK("verify:drupal:cron-queue", fmt.Sprintf("cron available; %d configured queue workers discovered", *probe.QueueWorkers))
}

func verifyDrupalSolr(output string) sitevalidate.Result {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return verifyFailed("verify:drupal:solr", "Search API returned no server status", "check the default_solr_server configuration and Solr connectivity")
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return verifyFailed("verify:drupal:solr", fmt.Sprintf("invalid server-status JSON: %v", err), "run drush search-api:server-status and inspect the default_solr_server response")
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			return verifyFailed("verify:drupal:solr", "server-status returned an empty object", "check the default_solr_server configuration and Search API Solr compatibility")
		}
	case []any:
		if len(typed) == 0 {
			return verifyFailed("verify:drupal:solr", "server-status returned an empty collection", "check the default_solr_server configuration and Search API Solr compatibility")
		}
	default:
		return verifyFailed("verify:drupal:solr", "server-status did not return a JSON object or collection", "inspect Search API Solr command compatibility")
	}
	return verifyOK("verify:drupal:solr", "Search API reached the default Solr server")
}

func verifyOK(name, detail string) sitevalidate.Result {
	return sitevalidate.Result{Name: name, Status: sitevalidate.StatusOK, Detail: detail}
}

func verifyFailed(name, detail, fix string) sitevalidate.Result {
	return sitevalidate.Result{Name: name, Status: sitevalidate.StatusFailed, Detail: detail, FixHint: fix}
}
