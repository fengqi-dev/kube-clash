# Third-party notices

## sing-box

KubeLoop is designed to distribute and run sing-box as a separate managed process.

- Project: https://github.com/SagerNet/sing-box
- Pinned version: v1.13.14
- License: GNU General Public License v3.0
- Source: https://github.com/SagerNet/sing-box/tree/v1.13.14

sing-box binaries are downloaded on first connect (or provided via
`KUBELOOP_SINGBOX_PATH`). Before a release bundles those binaries, the packaging
process must include the complete GPLv3 license text, retain upstream notices,
and provide the corresponding source in a manner compliant with GPLv3.

Windows builds also require `wintun.dll` from the sing-box release archive.
