# sitectl-drupal

`sitectl-drupal` simplifies the creation and operation of repositories created using the [LibOps Drupal template](https://github.com/libops/drupal). It provides sitectl commands for Drush, development mode, config sync, login links, validation, health checks, and component-driven customization.

Documentation: https://sitectl.libops.io/plugins/drupal

## Requirements

- [`sitectl`](https://sitectl.libops.io/install) v1.10.0 or newer, using RPC protocol 1.
- Docker with the Compose v2 plugin for local Drupal sites.
- No additional app-plugin dependency beyond core `sitectl`.
- Drupal template v1.2.0 or newer for the versioned rollout and verification programs required by `sitectl-drupal` v1.4.0 and newer.
- [`crosswalk`](https://github.com/lehigh-university-libraries/crosswalk) on `PATH`, or configured with `SITECTL_CROSSWALK_BINARY`, when using the optional metadata profile and service commands.

## Quick Start

Create a local Drupal site from the matching template:

```bash
sitectl create drupal/default \
  --template-repo https://github.com/libops/drupal \
  --path ./my-drupal-site \
  --type local \
  --checkout-source template \
  --default-context
```

The template README is at https://github.com/libops/drupal.

## Basic Operations

Use [`sitectl compose`](https://sitectl.libops.io/commands/compose) to start or inspect the stack:

```bash
sitectl compose up --remove-orphans -d
```

Use [`sitectl healthcheck`](https://sitectl.libops.io/commands/healthcheck) and [`sitectl validate`](https://sitectl.libops.io/commands/validate) to check the site:

```bash
sitectl healthcheck
sitectl verify --strict
sitectl validate
```

Use `sitectl deploy` for application updates. Before it stops the current containers, the Drupal plugin verifies that the checkout contains the template's readiness, migration, and application-verification programs and mounts them read-only at their stable container paths. If a checkout predates template v1.2.0, update it from the [LibOps Drupal template](https://github.com/libops/drupal) first and rerun the deploy; the plugin does not substitute inline fallback code.

Use [`sitectl image`](https://sitectl.libops.io/commands/image) for local image or build-arg overrides:

```bash
sitectl image set --tag drupal=nginx-1.30.3-php84
```

Use [`sitectl set`](https://sitectl.libops.io/commands/set) for component changes; it updates component-owned files immediately:

```bash
sitectl set dev-mode enabled
```

See the [Drupal plugin docs](https://sitectl.libops.io/plugins/drupal) for Drush, sync, ULI, and Drupal-specific workflows.

## License

`sitectl-drupal` is licensed under the MIT License.
