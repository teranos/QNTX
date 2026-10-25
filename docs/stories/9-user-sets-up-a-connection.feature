Feature: USER sets up a connection

  Scenario: USER sets up a connection in the Connector element
    Given I am in the Connector element
    When I pick a namespace, name the connection, and give the return address the app shows me
    And I press Connector
    Then I see the server URL, the client id and the client secret, without opening the token element
