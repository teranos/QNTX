
[[ADR-048-Node-Agent]]
[[ADR-049-Obsidian]]
[[ADR-052-Approvals]]
[[ADR-053-Namespace-Agents]]
[[ADR-055-Agent-Memory]]


pre-0.999.0-polish-blocker 

Agent rooms

Inside of a QNTX Cluster, only one Node is ever responsible for Rooms Servicees cluster-wide

think buzz.xyz

think [[Agent Swarm]]s

think telegram, slack, etc. 


think shared message board

think Agent participation in one or multiple rooms (at the same time)



think, creating a new abstraction about Rooms as a Plugin Provided service

think, switch core + adapter as a plugin that provides us the Room abstraction.
think, same thing for buzz.xyz

https://docs.switchagents.ai/switch-rooms/internals/agent-connection#connect-over-the-websocket

A Plugin Provided Rooms Service can use the existing QNTX managed harnesses.

Say each **element** is also a room, and who are in that room are the relevant Element Agent and its Story Elements.

Say there is a Deployment Room, PR's can be sent to it, and the room specific rule is that there is always 1 Agent assigned to doing those PR deploy's 

Say the Agy, Claude, Codex Node Agents are all in the Node Room 

Say you can wake a specific Agent so it will check the room

Say an Agent session is still a session as you know, but it communicates with the room, or rather it has access to it. 

Say a Node Agent is allowed to write into Element Room's but Element Agents cant write into Node Room

Say there is a Room, and there is a PR, updates to that PR should arrive in the relevant Room. Including things like CI and comments. 
	- https://github.com/teranos/QNTX/pull/1607
	- https://github.com/teranos/QNTX-Web/pull/100
	- 

Say there is a Room per Namespace 


An Agent should be able to Involve other Agents (it can reach) into a Room that Agent can access



