Feature: USER connects Obsidian

  "for example the obsidian element, you would access it from connectors"

  Scenario: USER opens Obsidian from Connectors
    Given I am in Connectors
    When I press Obsidian
    Then the Obsidian element opens, as ADR-049 says
