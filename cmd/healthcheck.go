package cmd

import "github.com/libops/sitectl/pkg/plugin"

var drupalHealthcheckRunner = plugin.StandardComposeWebHealthcheck(plugin.StandardComposeWebHealthcheckOptions{
	AppService:              "drupal",
	HTTPName:                "http:drupal",
	DefaultScheme:           "http",
	DefaultDomain:           "localhost",
	DatabaseService:         "mariadb",
	CheckDatabaseDependency: true,
	SolrService:             "solr",
	SolrCore:                "default",
})
