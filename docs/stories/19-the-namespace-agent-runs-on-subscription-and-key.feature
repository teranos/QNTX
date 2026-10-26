Feature: The namespace agent runs on subscription and key

  "or you could say, if subscription runs out, you use the API"

  "and its not OR"

  "its BOTH"

  "CLAUDE CODE INSIDE OF QNTX"

  Scenario: The agent runs on the subscription
    Given the agent of my namespace is signed in with my Claude subscription
    When I say something to it
    Then it runs on my Claude subscription

  Scenario: The agent runs on the key when the subscription runs out
    Given my Claude subscription ran out
    And my namespace holds my anthropic key
    When I say something to the agent
    Then it runs on my anthropic key
