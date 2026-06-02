# sitectl-drupal

A [sitectl](https://github.com/libops/sitectl) plugin for drupal websites.

## Install

### Homebrew

You can install `sitectl-drupal` using homebrew

```bash
brew tap libops/homebrew https://github.com/libops/homebrew
brew install libops/homebrew/sitectl-drupal
```

### Download Binary

Instead of homebrew, you can download a binary for your system from [the latest release of sitectl](https://github.com/libops/sitectl/releases/latest) and [this plugin](https://github.com/libops/sitectl-drupal/releases/latest)

Then put the binary in a directory that is in your `$PATH`

## Commands

- `sitectl drupal build`
- `sitectl drupal init`
- `sitectl drupal up`
- `sitectl drupal down`
- `sitectl drupal status`
- `sitectl drupal logs [SERVICE...]`
- `sitectl drupal rollout`
- `sitectl drupal drush [COMMAND...]`
- `sitectl drupal uli`
- `sitectl drupal sync database`
- `sitectl drupal sync config`
