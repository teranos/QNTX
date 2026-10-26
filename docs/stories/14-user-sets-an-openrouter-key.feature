Feature: USER sets an OpenRouter key

  "but only one is ever active for the namespace agent"

  Scenario: USER sets the OpenRouter key
    Given I am in Connectors
    When I press OpenRouter
    And I give my OpenRouter API key
    Then the namespace I am standing in keeps it sealed, as ADR-051 says
    And I am never shown the key again

  Scenario: The key set last is the one the namespace agent uses
    Given I set an OpenRouter key before
    When I set another
    Then the namespace agent uses the one I set last

  Scenario Outline: USER sets the <provider> key the same way
    Given I am in Connectors
    When I press <provider>
    And I give that key
    Then the namespace agent in Pi calls <provider> with its own key, as ADR-053 says

    Examples:
      | provider  |
      | Anthropic |
      | OpenAI    |
