Feature: USER sets up a connection

  Scenario: USER sees what can be connected
    Given I am standing in a namespace
    When I open Connectors
    Then I see ChatGPT, Claude, Obsidian, OpenRouter and GitHub, each with its own logo and colors
    And I am not asked which namespace

  Scenario: Each connection leads to its own story
    Given I am in Connectors
    When I press one
    Then its own story starts
