Feature: USER sets up a connection

  "THE USER SETS UP their CONNECTIONS"

  "AS A USER I EXPECT TO be able to get to THE CONNECTORS ELEMENT"

  "IN THE CONNECTORS ELEMENT, IF THE SYSTEM CAN COMPUTE information FOR ME, I EXPECT it to compute for me."
  "If the system can compute it for the USER, the USER isn't expected to know and have perfect information"

  "EACH CONNECTOR LEADS TO THEIR OWN STORY."

  Scenario: USER sees what can be connected
    Given I am standing in a namespace
    When I open Connectors
    Then I see rows for each platform we can integrate with, each with its own logo and color

  Scenario Outline: Each connection leads to its own story
    Given I am in Connectors
    When I press <platform>
    Then <story> starts

    Examples:
      | platform | story                                  |
      | Obsidian   | 13-user-connects-obsidian.feature      |
      | OpenRouter | 14-user-sets-an-openrouter-key.feature |
      | Anthropic  | 12-user-connects-anthropic.feature |
      | OpenAI     | 11-user-connects-openai.feature |
      | GitHub     | 5-user-connects-github.feature         |
