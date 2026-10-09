Feature: USER sets up a new namespace

  Scenario: USER goes through the namespace setup wizard
    Given I am signed in
    When I set up a namespace
    Then I go through a small setup wizard, similar to the first-time node setup, but for a namespace
    And at some point the namespace part is done

  Scenario: USER skips the wizard
    Given I am in the small setup wizard
    When I skip it
    Then the namespace part is done
