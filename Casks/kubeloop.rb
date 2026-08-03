cask "kubeloop" do
  arch arm: "arm64", intel: "amd64"

  version "1.6.1"
  sha256 arm:   "451f792df834d4d1a8e614a5984532193bbf9c0e4ca4a39f3ad169dd73b370f9",
         intel: "3e7570b7ebd9286e3356691e9532806eb6bc3c333bbce3aff84cc10cdc9cf729"

  url "https://github.com/fengqi-dev/kube-loop/releases/download/v#{version}/kubeloop-#{version}-darwin-#{arch}.dmg"
  name "KubeLoop"
  desc "Connect your laptop to Kubernetes like a VPN"
  homepage "https://fengqi-dev.github.io/kube-loop/"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: :big_sur

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
