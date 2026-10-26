Feature: USER connects OpenAI

  Scenario: USER connects ChatGPT
    Given I am in Connectors
    And I see the server MCP URL of the namespace I am standing in
    When I press OpenAI
    And I press ChatGPT
    Then I am told to go to https://chatgpt.com/plugins
    And I am shown where to click there: Add, then Add custom MCP server
    When I give the callback URL ChatGPT shows me, which ChatGPT makes per connector
    Then I get the client id, the client secret and the token endpoint auth method, right there
    When I set ChatGPT up with those, and say yes at my passkey
    Then ChatGPT reaches the namespace I am standing in

  Scenario: USER connects a second ChatGPT account
    Given I connected ChatGPT
    When I connect a second ChatGPT account the same way
    Then it is its own connection
