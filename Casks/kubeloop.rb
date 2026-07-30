cask "kubeloop" do
  arch arm: "arm64", intel: "amd64"

  version "1.1.1"
  sha256 arm:   "d9fde4aca22ae0ec653768dafb062ee890f9a1eea527de8d0effed8bc6cdd099",
         intel: "33dbc940fc1ee19ad8a031d2d135243c8d1a9bdb4bbb1adff982fd51d5a1c4a2"

  url "https://github.com/fengqi-dev/kube-loop/releases/download/v#{version}/kubeloop-#{version}-darwin-#{arch}.dmg"
  name "KubeLoop"
  desc "Connect your laptop to Kubernetes like a VPN"
  homepage "https://fengqi-dev.github.io/kube-loop/"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: ">= :big_sur"

  app "KubeLoop.app"

  # Unsigned / unnotarized builds trip Gatekeeper via com.apple.quarantine.
  # Clear it after install so users do not need a manual xattr.
  postflight do
    system_command "/usr/bin/xattr",
                   args: ["-dr", "com.apple.quarantine", "#{appdir}/KubeLoop.app"],
                   sudo: false
  end

  zap trash: [
    "~/.kubeloop",
    "~/Library/Application Support/KubeLoop",
    "~/Library/Caches/KubeLoop",
    "~/Library/Preferences/com.wails.kube-loop.plist",
    "~/Library/Saved Application State/com.wails.kube-loop.savedState",
  ]

  caveats <<~EOS
    On first use, approve the virtual network helper when prompted.
    You can uninstall it later from Settings.
  EOS
end
