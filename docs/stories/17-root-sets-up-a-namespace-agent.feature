Feature: ROOT sets up a namespace agent

  "ROOT sets up a namespace agent"

  Scenario: ROOT sets up the agent of a namespace
    Given I am ROOT
    When I set up the agent of a namespace
    Then that namespace has its agent
