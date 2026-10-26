Feature: USER connects ChatGPT

  "you can connect multiple chatgpt accounts"

  Scenario: USER connects ChatGPT
    Given I am in Connectors
    When I press ChatGPT
    And I give the callback URL ChatGPT shows me, which ChatGPT makes per connector, so the node cannot know it
    Then I get the server URL, the client id, the client secret and the token endpoint auth method, right there
    When I set ChatGPT up with those, and say yes at my passkey
    Then ChatGPT reaches the namespace I am standing in

  Scenario: USER connects a second ChatGPT account
    Given I connected ChatGPT
    When I connect a second ChatGPT account the same way
    Then it is its own connection
