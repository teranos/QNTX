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
    timeout-minutes = 110;
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
        "with".targets = "aarch64-apple-ios-sim, wasm32-unknown-unknown";
      }
      {
        uses = "jetli/wasm-pack-action@v0.4.0";
        "with".version = "v0.13.1";
      }
      {
        uses = "oven-sh/setup-bun@v2";
        "with".bun-version = "1.3.3";
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
        name = "A bare page without the viewport line: the web view zooms, and this run sees it";
        # A case that hangs ends here, its log kept, and the next case still runs.
        timeout-minutes = 20;
        run = ''
          mkdir -p qntx/internal/server/dist
          sed 's|<!-- VIEWPORT -->|<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">|' qntx/ci/app-keyboard/page.html > qntx/internal/server/dist/index.html
          bash qntx/ci/app-keyboard/run.sh without
        '';
      }
      {
        "if" = "always()";
        name = "The same page, with html and body pinned to the window and not scrolling";
        # A case that hangs ends here, its log kept, and the next case still runs.
        timeout-minutes = 20;
        run = ''
          (cd qntx && make web)
          python3 -c 'import sys; p=sys.argv[1]; s=open(p).read(); a=s.index("<meta name=\"viewport\""); b=s.index(">", a)+1; s=s[:a]+sys.argv[2]+s[b:]; c=s.index("</head>"); open(p,"w").write(s[:c]+"<style>html, body { height: 100%; overflow: hidden; }</style>"+s[c:])' qntx/internal/server/dist/index.html '<meta name="viewport" content="width=device-width, initial-scale=1, minimum-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover">'
          bash qntx/ci/app-keyboard/run.sh pinned
        '';
      }
      {
        "if" = "always()";
        name = "QNTX's page as main builds it, holding where its window stood while a field has the keyboard";
        # Pinned, the window still went from 778 to 743 tall and the page scrolled
        # 35 to match, in a shell with or without the web view's inset adjustment.
        timeout-minutes = 20;
        run = ''
          (cd qntx && make web)
          python3 -c 'import sys; p=sys.argv[1]; s=open(p).read(); c=s.index("</head>"); open(p,"w").write(s[:c]+open(sys.argv[2]).read()+s[c:])' qntx/internal/server/dist/index.html qntx/ci/app-keyboard/hold-page.html
          bash qntx/ci/app-keyboard/run.sh page
        '';
      }
      {
        "if" = "always()";
        name = "What the screen said";
        run = ''
          cat verdicts.txt
          grep -q '^without: zoomed$' verdicts.txt || echo "without the line the page did not zoom"
          grep -q '^pinned: stayed$' verdicts.txt || echo "pinned, QNTX's page did more than bring the keyboard"
          grep -q '^page: stayed$' verdicts.txt
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
