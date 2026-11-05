cask "nib" do
  version "0.7.0"
  sha256 "d79df5dc046b434ee7c0b0f1aecb132e5cbd648d67b771f6aaca3dda3478502a"

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
