cask "nib" do
  version "0.6.4"
  sha256 "ddf7b1497e9e694af2bde4763ecb2f7c500d18050cfbb684280dc172f57f0cb1"

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
