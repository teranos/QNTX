Feature: USER connects Anthropic

  "QNTX AS A CONNECTOR IN YOUR CLAUDE CODE"

  "CLAUDE CODE INSIDE OF QNTX"

  "AND THE API KEY"

  Scenario Outline: Anthropic leads to three stories
    Given I am in Connectors
    When I press Anthropic
    And I press <part>
    Then <story> starts

    Examples:
      | part              | story                                                     |
      | add QNTX MCP to Claude | 20-user-adds-qntx-mcp-to-claude.feature              |
      | Claude Code       | 22-user-signs-claude-code-in.feature                      |
      | Anthropic API key | 21-user-sets-the-anthropic-key.feature                    |
