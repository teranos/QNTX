Feature: ROOT changes a user's level

  Scenario: ROOT changes what level a user is
    Given I am ROOT
    And a user is at level PUBLIC_REGISTRATION
    When I open the Users element
    And I open that user
    And I change their level to USER
    Then that user is at level USER
