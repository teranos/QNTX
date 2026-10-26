Feature: USER signs Claude Code in

  "CLAUDE CODE INSIDE OF QNTX"

  Scenario: USER signs Claude Code in with their own subscription
    Given I am in Connectors
    When I press Anthropic
    And I press Claude Code
    Then I go through the claude login flow, using my own subscription, in a way that is compliant
    And Claude Code inside QNTX is signed in, in the namespace I am standing in
