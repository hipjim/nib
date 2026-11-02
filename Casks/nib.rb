cask "nib" do
  version "0.6.2"
  sha256 "a75de6b18ac6145e9dc04022ce071469815efaa7c7b0c704d551b8f37dd8cd50"

  url "https://github.com/hipjim/nib/releases/download/v#{version}/nib-v#{version}-macos-universal.zip"
  name "nib"
  desc "Small, fast, GPU-rendered text editor"
  homepage "https://github.com/hipjim/nib"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: :monterey

  app "nib.app"
  binary "#{appdir}/nib.app/Contents/MacOS/nib"

  zap trash: "~/Library/Application Support/nib"
end
