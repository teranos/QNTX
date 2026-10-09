Feature: ROOT invites a user

  Scenario: ROOT sends an invite link to a friend
    Given I am ROOT
    When I open the Users element
    And I enter my friend's e-mail address
    And I send the invite link
    Then a mail goes out to my friend
    And a mail goes out to me
    And the mail I received has a button for cancelling the invitation

  Scenario: The friend opens the mail
    Given my friend has the mail
    When my friend clicks the link
    Then my friend chooses one of the identity providers, like google
    And my friend does not set a password

  Scenario: ROOT cancels the invitation
    Given I am ROOT
    And I have the mail with the cancel button
    When I press the button
    Then the invitation is cancelled
