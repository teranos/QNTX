Feature: USER sets up a connection

  "THE USER SETUP'S UP CONNECTIONS"

  "AS A USER I EXPECT TO SEE THE CONNECTORS ELEMENT"

  "AND IN THE CONNECTORS ELEMENT, IF THE SYSTEM CAN COMPUTE IT FOR ME, I EXPECT NOT TO HAVE TO TYPE AND FILL IN DETAILS"

  "EACH LEADING UP TO THEIR OWN STORY."

  Scenario: USER sees what can be connected
    Given I am standing in a namespace
    When I open Connectors
    Then I see ChatGPT, Claude, Obsidian, OpenRouter, Anthropic, OpenAI and GitHub, each with its own logo and colors
    And I am not asked which namespace

  Scenario Outline: Each connection leads to its own story
    Given I am in Connectors
    When I press <connection>
    Then <story> starts

    Examples:
      | connection | story                                  |
      | ChatGPT    | 11-user-connects-chatgpt.feature       |
      | Claude     | 12-user-connects-claude.feature        |
      | Obsidian   | 13-user-connects-obsidian.feature      |
      | OpenRouter | 14-user-sets-an-openrouter-key.feature |
      | Anthropic  | 14-user-sets-an-openrouter-key.feature |
      | OpenAI     | 14-user-sets-an-openrouter-key.feature |
      | GitHub     | 5-user-connects-github.feature         |
