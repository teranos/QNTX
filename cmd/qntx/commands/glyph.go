package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/glyph"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
)

// A glyph's UI is an attestation, and this is how one is written.
//
// Publishing reaches a node over HTTP rather than a store on disk, because the
// node whose canvas a glyph appears on is usually not the machine it was built
// on. The node serves what is published from /g/, and every page in that
// namespace redraws without a reload.

// GlyphCmd is the glyph verb.
var GlyphCmd = &cobra.Command{
	Use:   "glyph",
	Short: "Publish the UI a glyph is",
	Long: `Publish and list glyph modules.

A glyph's module is an ES module published as an attestation — subject
glyph-<name>, predicate ` + glyph.ModulePredicate + `, the source under the ` + glyph.SourceAttribute + `
attribute. The node serves what is published from /g/<name>.js, so a page
imports it same-origin and no build of the node has to change for the UI to.

Publishing again supersedes: the standing watcher every node is born with
notices, and pages in that namespace redraw the glyph where it stands.`,
}

var glyphPublishCmd = &cobra.Command{
	Use:   "publish NAME",
	Short: "Publish a glyph module, or replace the one standing",
	Long: `Publish a glyph module built as a single ES file.

The module exports glyphDef (symbol, title, label, and manifestation 'panel'
for the tray) and render(glyph, ui). What it is published as is its name here,
not anything inside the file.

Examples:
  qntx glyph publish crier --file dist/glyph-module.js
  qntx glyph publish crier --file dist/glyph-module.js --to https://api.q.abcd.nl`,
	Args: cobra.ExactArgs(1),
	RunE: runGlyphPublish,
}

var glyphListCmd = &cobra.Command{
	Use:   "list",
	Short: "What a node serves from /g/",
	Long: `List the glyph modules a node publishes, newest first by name.

Answers for the namespace the token acts in — a glyph published in one
namespace is not served to another.`,
	Args: cobra.NoArgs,
	RunE: runGlyphList,
}

var (
	glyphFile  string
	glyphTo    string
	glyphToken string
)

func init() {
	glyphPublishCmd.Flags().StringVar(&glyphFile, "file", "", "Path to the built module (required)")
	if err := glyphPublishCmd.MarkFlagRequired("file"); err != nil {
		panic(err) // A flag named here and not on the command is a build mistake.
	}

	for _, c := range []*cobra.Command{glyphPublishCmd, glyphListCmd} {
		c.Flags().StringVar(&glyphTo, "to", "", "Node to reach (default: this machine's, from am.toml)")
		c.Flags().StringVar(&glyphToken, "token", "", "Bearer token (default: $QNTX_TOKEN, then ~/.qntx/token)")
	}

	GlyphCmd.AddCommand(glyphPublishCmd, glyphListCmd)
}

// nodeURL is the node a command reaches: what --to said, or the one this
// machine runs.
func nodeURL() string {
	if glyphTo != "" {
		return strings.TrimSuffix(glyphTo, "/")
	}
	return fmt.Sprintf("http://127.0.0.1:%d", config.GetServerPort())
}

// bearer is the token to present, and where it came from, so a refusal can say
// which credential was refused.
func bearer() (token string, from string, err error) {
	if glyphToken != "" {
		return glyphToken, "--token", nil
	}
	if fromEnv := os.Getenv("QNTX_TOKEN"); fromEnv != "" {
		return fromEnv, "$QNTX_TOKEN", nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", errors.Wrap(err, "no --token, no $QNTX_TOKEN, and the home directory is unknown")
	}
	path := filepath.Join(home, ".qntx", "token")
	held, err := os.ReadFile(path)
	if err != nil {
		return "", "", errors.Wrapf(err,
			"no --token and no $QNTX_TOKEN, and %s could not be read", path)
	}
	return strings.TrimSpace(string(held)), path, nil
}

// ask sends one request to the node and hands back what it answered.
func ask(method, url string, body []byte) (answer []byte, err error) {
	token, from, err := bearer()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrapf(err, "could not build the request to %s", url)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not answer", url)
	}
	defer func() { err = sqlclose.With(err, resp.Body.Close(), "the response body") }()

	answer, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, errors.Wrapf(readErr, "%s answered %d and the body could not be read", url, resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return nil, errors.Newf("%s answered %d (token from %s): %s",
			url, resp.StatusCode, from, strings.TrimSpace(string(answer)))
	}
	return answer, nil
}

func runGlyphPublish(cmd *cobra.Command, args []string) error {
	name := args[0]
	if strings.ContainsAny(name, "/ .") {
		return errors.Newf("a glyph name is a bare word, got %q", name)
	}

	source, err := os.ReadFile(glyphFile)
	if err != nil {
		return errors.Wrapf(err, "the module could not be read from %s", glyphFile)
	}
	if len(source) == 0 {
		return errors.Newf("%s is empty; publishing it would serve a module exporting nothing", glyphFile)
	}

	body, err := json.Marshal(map[string]any{
		"subjects":   []string{glyph.SubjectPrefix + name},
		"predicates": []string{glyph.ModulePredicate},
		"contexts":   []string{"_"},
		"source":     "qntx-cli",
		"attributes": map[string]string{glyph.SourceAttribute: string(source)},
	})
	if err != nil {
		return errors.Wrap(err, "the attestation could not be written as JSON")
	}

	to := nodeURL()
	answer, err := ask(http.MethodPost, to+"/api/attestations", body)
	if err != nil {
		return err
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(answer, &created); err != nil {
		return errors.Wrapf(err, "%s took the module and answered something unreadable: %s", to, answer)
	}

	fmt.Printf("✓ %s published as %s (%d bytes)\n", name, created.ID, len(source))
	fmt.Printf("  served from %s/g/%s.js\n", to, name)
	fmt.Printf("  pages in this namespace redraw it where it stands\n")
	return nil
}

func runGlyphList(cmd *cobra.Command, args []string) error {
	to := nodeURL()
	answer, err := ask(http.MethodGet, to+"/g/", nil)
	if err != nil {
		return err
	}

	var served struct {
		Glyphs []struct {
			Name      string    `json:"name"`
			As        string    `json:"as"`
			URL       string    `json:"url"`
			Published time.Time `json:"published"`
			By        []string  `json:"by"`
		} `json:"glyphs"`
	}
	if err := json.Unmarshal(answer, &served); err != nil {
		return errors.Wrapf(err, "%s answered something that is not a glyph listing: %s", to, answer)
	}

	if len(served.Glyphs) == 0 {
		fmt.Printf("%s publishes no glyphs\n", to)
		return nil
	}

	sort.Slice(served.Glyphs, func(i, j int) bool {
		return served.Glyphs[i].Name < served.Glyphs[j].Name
	})
	for _, g := range served.Glyphs {
		fmt.Printf("%-16s %s  %s\n", g.Name, g.As, g.Published.Format(time.RFC3339))
		fmt.Printf("%-16s %s%s\n", "", to, g.URL)
		if len(g.By) > 0 {
			fmt.Printf("%-16s by %s\n", "", strings.Join(g.By, ", "))
		}
	}
	return nil
}
