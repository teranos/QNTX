# Future Vision Items to Move to GitHub Issues

These items should be moved from documentation to GitHub issues to keep docs focused on current reality:

## From config-panel.md

### Issue: Multi-Provider Config Sources
**Description**: Support for external configuration providers
- HashiCorp Vault integration for secrets rotation
- Consul for dynamic service configuration
- AWS Secrets Manager support
- etcd for distributed configuration
- Provider health indicators with refresh timestamps
- Config timeline showing when values changed

**Labels**: enhancement, configuration, future

### Issue: Documentation Drawer
**Description**: In-panel documentation for configuration options
- Click any config key → right side slides in with docs
- Show description, type, valid values, examples
- Related settings cross-references
- Infrastructure exists, needs content population

**Labels**: enhancement, documentation, UI

## From task-logging.md

### Issue: Real-time Log Streaming
**Description**: Stream logs as they're generated
- WebSocket-based log streaming
- Filterable by job, stage, level
- Integration with existing pulse WebSocket infrastructure

**Labels**: enhancement, logging, real-time

### Issue: Advanced Log Filtering
**Description**: Sophisticated log search and filtering
- Regex pattern matching
- Time range filtering
- Export functionality
- Log aggregation views

**Labels**: enhancement, logging

## From pulse-async-ix.md

### Issue: User-Defined Retention Policies
**Description**: Allow custom retention rules for attestations
- Importance-based retention
- Time-based rules
- Entity-type specific policies
- Configurable per domain

**Labels**: enhancement, storage, attestations

## From Installation Guide

### Issue: Package Manager Support
**Description**: Distribute QNTX through standard package managers
- Homebrew formula for macOS/Linux
- winget package for Windows
- APT repository for Debian/Ubuntu
- RPM package for Fedora/RHEL

**Labels**: enhancement, distribution

## From config-system.md

### Issue: Configuration Enhancements
**Description**: Future config system improvements
- Config validation with warnings before save
- Reset to defaults UI button
- Export merged configuration
- Config diff view showing changes from defaults
- Multi-environment support (dev/staging/prod profiles)

**Labels**: enhancement, configuration

## Instructions for Creating Issues

When creating these GitHub issues:

1. Use clear, actionable titles
2. Reference the original documentation file
3. Mark as "enhancement" not "bug"
4. Add "future-vision" label to distinguish from immediate work
5. No milestone assignment (these are aspirational)
6. Link related issues where applicable

Example issue format:

```markdown
## Summary
[Brief description of the feature]

## Motivation
[Why this would be valuable]

## Proposed Solution
[High-level approach]

## Alternatives Considered
[Other approaches that might work]

## Additional Context
Originally documented in: `docs/[filename].md`
Related to: #[issue-number]
```