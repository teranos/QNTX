Feature: USER connects GitHub

  Scenario: USER connects GitHub during namespace setup
    Given I am at the GitHub step of namespace setup
    When I press Connect
    Then GitHub asks me whether to connect my GitHub account
    When I say yes
    Then I am back in setup, connected as my GitHub login

  Scenario: USER skips GitHub during namespace setup
    Given I am at the GitHub step of namespace setup
    When I skip it
    Then I am at the next step
