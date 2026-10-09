Feature: USER sets up the namespace agent

  Agent setup is its own thing, skippable as part of namespace creation, but still something you probably want to setup

  Scenario: USER sets up the agent with the claude login flow
    Given I am at agent setup
    When I choose claude
    Then I go through the claude login flow, using my own subscription, in a way that is compliant
    And the agent is governed by ground and its pbt syntax
    And I am not involved with the pbt, because things just work

  Scenario: USER sets up the agent with pi
    Given I am at agent setup
    When I choose pi
    Then I set it up with my own key
    And the agent is governed by ground and its pbt syntax
    And I am not involved with the pbt, because things just work

  Scenario: USER skips agent setup
    Given I am at agent setup, as part of namespace creation
    When I skip it
    Then namespace creation goes on without an agent
