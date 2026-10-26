Feature: ROOT changes a User's level

  Scenario: ROOT raises a User
    Given I am ROOT
    When I open a User from the Users element
    And I choose a level for them
    And I press Set level
    Then the User holds that level
    And the act is attested as identity:leveled, naming who, the level they had, and the level they hold now

  Scenario: ROOT is offered the levels the gate admits a User at
    Given I am ROOT
    When I open a User from the Users element
    Then I am offered USER and PUBLIC_REGISTRATION
    And I am not offered SUPER or ATTESTOR, which no gate admits a User at yet

  Scenario: Nobody but ROOT changes a level
    Given I am a User who is not ROOT
    When I ask the node to set another User's level
    Then the node refuses, and says a level is set by ROOT

  Scenario: There is one ROOT User
    Given I am ROOT
    When I ask the node to make a User ROOT
    Then the node refuses
    When I ask the node to change my own level
    Then the node refuses, as ADR-031 says
