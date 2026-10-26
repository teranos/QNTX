Feature: USER goes on to the namespace agent

  "THE NAMESPACE AGENT, IS A STORY YOU CAN OPTIONALLY END UP IN AFTER DOING THE CONNECTOR"

  Scenario: USER goes on to the namespace agent
    Given I connected Claude, as 20-user-adds-qntx-mcp-to-claude.feature says
    When I go on
    Then I am at the agent of the namespace I am standing in

  Scenario: USER stops after the connector
    Given I connected Claude, as 20-user-adds-qntx-mcp-to-claude.feature says
    When I stop there
    Then the connector is done, and no agent is set up
