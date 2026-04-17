# Publishing the APiX VS Code Extension

This guide covers how to publish the APiX extension to the [VS Code Marketplace](https://marketplace.visualstudio.com/).

## Prerequisites

- Node.js 18+ and npm
- A [Visual Studio Marketplace publisher account](https://marketplace.visualstudio.com/manage) with publisher ID `mnafshin`
- A Personal Access Token (PAT) from [Azure DevOps](https://dev.azure.com/) with the **Marketplace (publish)** scope

## One-time Setup

1. Install `@vscode/vsce` globally (or use the local dev dependency):
   ```bash
   npm install -g @vscode/vsce
   ```

2. Log in with your PAT:
   ```bash
   vsce login mnafshin
   # Paste your Azure DevOps PAT when prompted
   ```

## Building the VSIX

From the `apix-vscode/` directory:

```bash
npm install
npm run compile   # TypeScript → out/
npm run package   # creates apix-<version>.vsix
```

The `.vsix` file is created in `apix-vscode/`. You can install it locally with:

```bash
code --install-extension apix-<version>.vsix
```

## Publishing to the Marketplace

After logging in with `vsce login`:

```bash
cd apix-vscode
npm run publish   # runs: vsce publish
```

To publish a specific version bump:

```bash
vsce publish patch   # bumps patch version (1.0.0 → 1.0.1)
vsce publish minor   # bumps minor version (1.0.0 → 1.1.0)
vsce publish major   # bumps major version (1.0.0 → 2.0.0)
```

## CI/CD

The workflow `.github/workflows/publish-extension.yml` automatically builds and archives a `.vsix` artifact on every push to `main` that touches `apix-vscode/**`. This artifact can be downloaded from the GitHub Actions run and installed manually or used as a release asset.

To enable automated publishing, add your Azure DevOps PAT as a repository secret named `VSCE_PAT` and extend the workflow with:

```yaml
- name: Publish to Marketplace
  run: npm run publish
  env:
    VSCE_PAT: ${{ secrets.VSCE_PAT }}
```

## Version Management

The extension version is controlled by the `version` field in `apix-vscode/package.json`. Keep it in sync with the engine release tag when cutting a release.

## Marketplace Listing Requirements

Before publishing, ensure the following are present and accurate:
- `README.md` at the repo root (used as the marketplace description)
- `LICENSE` file
- `icon` field in `package.json` pointing to a 128×128 PNG
- `repository` and `bugs` URLs in `package.json`
- A non-empty `description` in `package.json`
