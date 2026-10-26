Feature: USER connects Claude

  Scenario: USER connects Claude
    Given I am in Connectors
    When I press Claude
    Then I type nothing, because Claude's callback is always https://claude.ai/api/mcp/auth_callback
    And I get the server URL, the client id and the client secret, right there
    When I set Claude up with those, and say yes at my passkey
    Then Claude reaches the namespace I am standing in
