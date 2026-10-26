Feature: USER runs the namespace agent in Pi

  "I want Pi to also serv the function of namespace agent alongside Claude code namespace agent"

  Scenario: USER sets up the namespace agent in Pi
    Given I am standing in a namespace
    When I set up its agent in Pi, naming the model, the thinking level and the provider
    Then its model calls are made at that provider on the namespace's key of the provider's name, as 14-user-sets-an-openrouter-key.feature says
    And the agent is governed by ground and its pbt syntax
    And I am not involved with the pbt, because things just work
