Feature: ROOT connects an app to the node

  "ROOT WANTS AN MCP OVER THE ENTIRE SERVER, SET VIA EITHER I OR AM ELEMENT"

  Scenario: ROOT mints ROOT's client on i or am
    Given I am ROOT
    And I see the server MCP URL
    When I name ROOT's client and give the return address the app shows me
    And I press ROOT's client
    Then I get the client id and the client secret

  Scenario: The app reaches the entire server as ROOT
    Given an app set up with ROOT's client
    When I say yes at my passkey
    Then the app reaches the node's MCP server as ROOT
