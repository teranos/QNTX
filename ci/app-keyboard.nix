# The app on an iPhone, and a tap on a box. .github/workflows/app-keyboard.yml
# is emitted from this and is never edited by hand:
#
#   nix eval --json --file ci/app-keyboard.nix | jq . > .github/workflows/app-keyboard.yml
#
# "you tap a box to type, and the only thing that happens is the keyboard
# coming up. No zoom, nothing moves, nothing refocuses. The same as Claude or
# ChatGPT on your phone."
#
# The app shows QNTX in Apple's web view, not Safari, so this builds the app's
# own shell (teranos/QNTX-App) for the Simulator, with QNTX beside it as the
# shell expects. Nothing here reaches TestFlight.
{
  name = "App keyboard";

  on = {
    pull_request.paths = [ "ci/app-keyboard/**" "ci/app-keyboard.nix" ".github/workflows/app-keyboard.yml" "web/index.html" ];
    workflow_dispatch = null;
  };

  jobs.ios = {
    name = "QNTX-App in the iPhone Simulator";
    runs-on = "macos-15";
    timeout-minutes = 60;
    steps = [
      {
        uses = "actions/checkout@v5";
        "with".repository = "teranos/QNTX-App";
      }
      {
        uses = "actions/checkout@v5";
        "with".path = "qntx";
      }
      {
        uses = "dtolnay/rust-toolchain@stable";
        "with".targets = "aarch64-apple-ios-sim";
      }
      {
        name = "Fetch tauri-cli 2.11.4";
        run = ''
          curl -sSL -o cargo-tauri.zip https://github.com/tauri-apps/tauri/releases/download/tauri-cli-v2.11.4/cargo-tauri-aarch64-apple-darwin.zip
          unzip -o -q cargo-tauri.zip -d "$HOME/.cargo/bin"
          rm cargo-tauri.zip
          cargo tauri --version
        '';
      }
      {
        uses = "actions/setup-python@v5";
        "with".python-version = "3.11";
      }
      {
        name = "A finger, and eyes";
        run = ''
          curl -sSL https://github.com/facebook/idb/releases/download/v1.1.8/idb-companion.universal.tar.gz | tar -xz
          COMPANION=$(dirname "$(find "$PWD" -name idb_companion -type f -perm -u+x | head -1)")
          echo "$COMPANION" >> "$GITHUB_PATH"
          python -m pip install --quiet fb-idb pillow
        '';
      }
      { run = "cargo tauri ios init"; }
      {
        name = "Boot an iPhone, with its own keyboard";
        run = ''
          defaults write com.apple.iphonesimulator ConnectHardwareKeyboard -bool false
          RUNTIME=$(xcrun simctl list runtimes available -j | jq -r '[.runtimes[] | select(.platform == "iOS")] | sort_by(.version) | last | .identifier')
          UDID=$(xcrun simctl list devices available -j | jq -r --arg rt "$RUNTIME" '[.devices[$rt][] | select(.name | startswith("iPhone"))] | first | .udid')
          echo "UDID=$UDID" >> "$GITHUB_ENV"
          xcrun simctl boot "$UDID"
          xcrun simctl bootstatus "$UDID" -b
          open -a Simulator --args -CurrentDeviceUDID "$UDID"
        '';
      }
      {
        name = "Without the line: the app's web view zooms on a 12px box, and this sees it";
        run = ''bash qntx/ci/app-keyboard/run.sh without "width=device-width, initial-scale=1, viewport-fit=cover"'';
      }
      {
        "if" = "always()";
        name = "With QNTX's own line";
        run = ''
          LINE=$(python3 -c 'import sys; s=open("qntx/web/index.html").read(); i=s.index("name=\"viewport\" content=\"")+len("name=\"viewport\" content=\""); print(s[i:s.index("\"", i)])')
          bash qntx/ci/app-keyboard/run.sh qntx "$LINE"
        '';
      }
      {
        "if" = "always()";
        name = "What the screen said";
        run = ''
          cat verdicts.txt
          grep -q '^without: zoomed$' verdicts.txt || { echo "without the line it did not zoom: this run cannot see a zoom"; exit 1; }
          grep -q '^qntx: not zoomed$' verdicts.txt || { echo "with QNTX's line the box still zoomed"; exit 1; }
        '';
      }
      {
        "if" = "always()";
        name = "The screens";
        uses = "actions/upload-artifact@v4";
        "with" = {
          name = "app-keyboard";
          path = "keyboard-shots";
          retention-days = 14;
        };
      }
    ];
  };
}
