Feature: USER sets up a new account

  Scenario: USER accepts the invitation
    Given I have a mail: ROOT invites you to QNTX, sign in with one of the providers ROOT specified
    When I press Accept the invitation
    Then I see only the sign-ins ROOT specified for me
    When I prove my account at one of them
    Then I am signed in
