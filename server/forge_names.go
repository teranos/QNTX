package server

// Before records named their forge, a repository was owner/repo, and the only
// forge a record could mean was GitHub. Every line written that way is named
// once, at start, by a newer line beside it; the older one is kept (ADR-026).
// Nothing reads the bare form after this: a repository names its forge.

import (
	"strings"

	"github.com/teranos/QNTX/internal/forge"
	errors "github.com/teranos/sacred-error"
)

// forgeNamer is who the lines that name a forge are by.
const forgeNamer = "forge-names"

// retiredLine marks a follow line that a newer one, under the name that now
// carries its forge, took the place of.
const retiredLine = "retired"

// legacyRepo is a repository written as owner/repo, before records named a
// forge, as the GitHub repository it was, and whether it was one. A name that
// already has a host is not one, and neither is one that is not owner/repo.
func legacyRepo(said string) (string, bool) {
	trimmed := strings.TrimSpace(said)
	if named, err := forge.ParseRepo(trimmed); err == nil {
		return named.String(), false
	}
	if strings.Count(trimmed, "/") != 1 {
		return trimmed, false
	}
	named, err := forge.ParseRepo(forge.GitHub + "/" + trimmed)
	if err != nil {
		return trimmed, false
	}
	return named.String(), true
}

// legacySource is a source written as owner/repo@branch[:path][=place], with
// the repository named by its forge. Whatever follows the repository is kept
// as it was.
func legacySource(said string) (string, bool) {
	repo, rest, found := strings.Cut(said, "@")
	if !found {
		return said, false
	}
	named, legacy := legacyRepo(repo)
	if !legacy {
		return said, false
	}
	return named + "@" + rest, true
}

// TODO(forge): Delete this file, its test and the call in Start once every node has started on a build that has it.
// nameForges names the forge of every repository the node's records hold as
// owner/repo. A record that cannot be read, or named, stops the node here: a
// reader that met it would refuse it anyway, and a node that went on without
// saying so would run on what it could not read.
func (s *QNTXServer) nameForges() error {
	if err := s.namePluginForges(); err != nil {
		return err
	}
	if err := s.nameVaultForges(); err != nil {
		return err
	}
	return s.nameFollowForges()
}

func (s *QNTXServer) namePluginForges() error {
	records, err := s.pluginRecords().Plugins()
	if err != nil {
		return errors.Wrap(err, "the plugin records were not read, so their repositories were not given their forge")
	}
	for _, record := range records {
		changed := false
		if core, held := record.Config[buildCore]; held {
			if named, legacy := legacySource(core); legacy {
				record.Config[buildCore], changed = named, true
			}
		}
		if inputs, held := record.Config[buildInputs]; held {
			fields := strings.Fields(inputs)
			for i, field := range fields {
				if named, legacy := legacySource(field); legacy {
					fields[i], changed = named, true
				}
			}
			if changed {
				record.Config[buildInputs] = strings.Join(fields, " ")
			}
		}
		if !changed {
			continue
		}
		if err := s.pluginRecords().pluginLine(forgeNamer, record, map[string]any{}); err != nil {
			return errors.Wrapf(err, "plugin %s was not given the forge of its build's repositories", record.Name)
		}
		s.logger.Infow("A plugin's build repositories are named by their forge", "plugin", record.Name, "core", record.Config[buildCore])
	}
	return nil
}

func (s *QNTXServer) nameVaultForges() error {
	vaults, err := s.nodeRecords().Vaults()
	if err != nil {
		return errors.Wrap(err, "the vaults were not read, so their repositories were not given their forge")
	}
	for _, vault := range vaults {
		changed := false
		for i, folder := range vault.Folders {
			if named, legacy := legacySource(folder); legacy {
				vault.Folders[i], changed = named, true
			}
		}
		if !changed {
			continue
		}
		if err := s.nodeRecords().SetVault(forgeNamer, vault); err != nil {
			return errors.Wrapf(err, "vault %s was not given the forge of its folders' repositories", vault.Name)
		}
		s.logger.Infow("A vault's folder repositories are named by their forge", "vault", vault.Name)
	}
	return nil
}

func (s *QNTXServer) nameFollowForges() error {
	follows, err := s.nodeRecords().Follows()
	if err != nil {
		return errors.Wrap(err, "the follows were not read, so their repositories were not given their forge")
	}
	for _, f := range follows {
		repo, repoLegacy := legacyRepo(f.Repo)
		dispatches, dispatchesLegacy := legacyRepo(f.Dispatches)
		if !repoLegacy && !dispatchesLegacy && f.kept == f.line() {
			continue
		}
		before := f.kept
		f.Repo, f.Dispatches = repo, dispatches
		if err := s.nodeRecords().SetFollow(forgeNamer, f); err != nil {
			return errors.Wrapf(err, "the follow %s was not given the forge of its repositories", before)
		}
		if before == f.line() {
			continue
		}
		if err := s.nodeRecords().nodeRecord(forgeNamer, githubSubject, before, "_", map[string]any{retiredLine: true}); err != nil {
			return errors.Wrapf(err, "the follow %s was named %s and not retired", before, f.line())
		}
		s.logger.Infow("A follow is kept under its own line, the old one retired", "was", before, "now", f.line())
	}
	return nil
}
