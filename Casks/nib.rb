cask "nib" do
  version "0.6.3"
  sha256 "bfca43597913ba6c5e732a3e45c500fba4568a8e6d23f87d64008aa6c72e616e"

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
