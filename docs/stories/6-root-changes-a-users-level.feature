Feature: ROOT changes a User's level

  Scenario: ROOT raises a User
    Given I am ROOT
    When I open a User from the Users element
    And I choose a level for them
    And I press Set level
    Then the User holds that level
    And the act is attested, naming me and the level they had

  Scenario: Nobody but ROOT changes a level
    Given I am a User who is not ROOT
    When I ask the node to set another User's level
    Then the node refuses, and says a level is set by ROOT

  Scenario: There is one ROOT User
    Given I am ROOT
    When I ask the node to make a User ROOT
    Then the node refuses
    When I ask the node to change my own level
    Then the node refuses
