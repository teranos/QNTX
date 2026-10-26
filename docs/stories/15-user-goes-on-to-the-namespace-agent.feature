Feature: USER goes on to the namespace agent

  "THE NAMESPACE AGENT, IS A STORY YOU CAN OPTIONALLY END UP IN AFTER DOING THE CONNECTOR"

  "you completed the anthropickey, you CAN end up here"

  "you dont choose, the system should know you camefrom either OpenAI, Google Agy, etc. etc..     the ystem computes what it cancompute"

  Scenario Outline: USER goes on to the namespace agent of <platform>
    Given I finished a story of <platform> in Connectors
    When I go on
    Then <story> starts, because the system knows I came from <platform>

    Examples:
      | platform  | story                                             |
      | Anthropic | 22-user-signs-claude-code-in.feature              |
      | OpenAI    | 23-user-runs-the-namespace-agent-in-codex.feature |
      | Google    | 24-user-runs-the-namespace-agent-in-agy.feature   |

  Scenario: USER stops there
    Given I finished a story of a platform in Connectors
    When I stop there
    Then no agent is set up
