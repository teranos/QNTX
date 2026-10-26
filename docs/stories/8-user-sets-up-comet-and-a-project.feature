Feature: USER sets up comet and a project

  Scenario: USER sets up ground comet on a project of their own
    Given the namespace and namespace setup part are done
    When I set up ground comet on a project on my own machine
    Then the same behaviour applies on that project
