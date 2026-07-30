Release and IDE builds place platform-specific helper binaries here before
building the desktop application:

- `kubeloop-helper[.exe]` — privileged service
- Windows also: `kubeloop-helper-install.exe`, `kubeloop-helper-uninstall.exe`

The desktop binary embeds them and materializes copies under
`~/.kubeloop/helper/resources/` when a packaged `resources\` directory is not
available (for example during `wails dev`).
