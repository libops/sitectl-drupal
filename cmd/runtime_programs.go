package cmd

const (
	drupalTemplateVersion = "v1.2.1"

	drupalRolloutPreflightSource  = "scripts/sitectl-rollout-preflight.sh"
	drupalRolloutPreflightCommand = "bash " + drupalRolloutPreflightSource
	drupalComposeConfigCommand    = "docker compose config --quiet"

	drupalWaitInstalledSource = "scripts/drupal-wait-installed.sh"
	drupalWaitInstalledTarget = "/usr/local/lib/sitectl/drupal-wait-installed.sh"
	drupalMigrationSource     = "scripts/drupal-rollout-migrate.sh"
	drupalMigrationTarget     = "/usr/local/lib/sitectl/drupal-rollout-migrate.sh"

	drupalVerifyConfigDriftSource = "scripts/drupal-verify-config-drift.php"
	drupalVerifyConfigDriftTarget = "/usr/local/lib/sitectl/drupal-verify-config-drift.php"
	drupalVerifyCronQueueSource   = "scripts/drupal-verify-cron-queue.php"
	drupalVerifyCronQueueTarget   = "/usr/local/lib/sitectl/drupal-verify-cron-queue.php"
	drupalVerifySolrSource        = "scripts/drupal-verify-solr.php"
	drupalVerifySolrTarget        = "/usr/local/lib/sitectl/drupal-verify-solr.php"
)

type drupalTemplateProgram struct {
	source     string
	target     string
	executable bool
}

var drupalTemplatePrograms = []drupalTemplateProgram{
	{source: drupalWaitInstalledSource, target: drupalWaitInstalledTarget, executable: true},
	{source: drupalMigrationSource, target: drupalMigrationTarget, executable: true},
	{source: drupalVerifyConfigDriftSource, target: drupalVerifyConfigDriftTarget},
	{source: drupalVerifyCronQueueSource, target: drupalVerifyCronQueueTarget},
	{source: drupalVerifySolrSource, target: drupalVerifySolrTarget},
}
