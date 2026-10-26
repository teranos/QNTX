Feature: USER connects Claude

  "QNTX AS A CONNECTOR IN YOUR CLAUDE CODE"

  Scenario: USER connects Claude
    Given I am in Connectors
    And I see the server MCP URL of the namespace I am standing in
    When I press Anthropic
    And I press Claude
    Then I type nothing, because Claude's callback is always https://claude.ai/api/mcp/auth_callback
    And I get the client id and the client secret, right there
    And I am told to go to https://claude.ai/new?modal=add-custom-connector#customize/connectors/yours
    When I set Claude up with those, and say yes at my passkey
    Then Claude reaches the namespace I am standing in
