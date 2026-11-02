cask "nib" do
  version "0.6.0"
  sha256 "7b50ad43eb570f7b0f510daaee2ddd98ea7a983ee885b781665f07ef36ca6f27"

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
