Feature: USER sets the Anthropic key

  "the API keys, brings you to platform, that is where the free money is."

  "why am i allowed to make a mistake in copying the key"

  Scenario: USER sets the Anthropic key
    Given I am in Connectors
    When I press Anthropic
    And I press Anthropic API key
    Then I am told to go to https://platform.claude.com/settings/keys
    When I give the key I made there
    Then the key is asked of Anthropic before it is kept
    And the namespace I am standing in keeps it sealed, as ADR-051 says
    And I am never shown the key again

  Scenario: A key Anthropic did not issue is not kept
    Given I am at the Anthropic API key
    When I give a key Anthropic did not issue
    Then it is not kept
    And I am told Anthropic did not issue it
