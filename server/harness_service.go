package server

import (
	"context"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	harnesswire "github.com/teranos/QNTX/plugin/harness"
	errors "github.com/teranos/sacred-error"
)

// 「[Fable 5.2, Fable 5.1] "Harness is a plugin that speaks grpc A2A" (ADR-059).」
//
// 「[Fable 5.2, Fable 5.1] The harness service is core's, one, as LLMServer is (ADR-014): a plugin that」
// 「[Fable 5.2, Fable 5.1] runs a harness declares harness_provider and serves A2A's A2AService on its」
// 「[Fable 5.2, Fable 5.1] port, and core routes a turn to the one the plugin's name names, through」
// 「[Fable 5.2, Fable 5.1] a2a-go's client over the connection core already holds to it.」

// 「[Fable 5.2, Fable 5.1] harnessPluginHeld is where a harness a plugin runs is said to be, as a」
// 「[Fable 5.2, Fable 5.1] binary's path is where one the node holds is.」
func harnessPluginHeld(name string) string { return "the " + name + " plugin" }

// 「[Fable 5.2, Fable 5.1] harnessRunning is a plugin that runs a harness: one that declared」
// 「[Fable 5.2, Fable 5.1] harness_provider at Initialize, and a2a-go's client of the A2AService it」
// 「[Fable 5.2, Fable 5.1] serves.」
type harnessRunning interface {
	IsHarnessProvider() bool
	A2AServiceClient() a2apb.A2AServiceClient
}

var _ harnessRunning = (*grpcplugin.ExternalDomainProxy)(nil)

// 「[Fable 5.2, Fable 5.1] harnessesAPluginHolds is each harness the node holds no binary of: its」
// 「[Fable 5.2, Fable 5.1] plugin's alone to run, and held where the plugin is. Claude Code is run by」
// 「[Fable 5.2, Fable 5.1] the claude plugin when one runs and by the node's own binary when none」
// 「[Fable 5.2, Fable 5.1] does, and what the node does with that binary beside a turn (a ritual's」
// 「[Fable 5.2, Fable 5.1] PATH, a login) needs the binary, so a plugin's label never stands for it.」
var harnessesAPluginHolds = map[string]bool{agentLinePi: true}

// 「[Fable 5.2, Fable 5.1] turnGivenUpWithin is how long core waits for a plugin to say a turn it gave」
// 「[Fable 5.2, Fable 5.1] up was canceled.」
const turnGivenUpWithin = 5 * time.Second

// 「[Fable 5.2, Fable 5.1] harnessBackend is the plugin that runs the harness named, when one does and」
// 「[Fable 5.2, Fable 5.1] is running: "a name that is not registered" is refused, and "no other」
// 「[Fable 5.2, Fable 5.1] provider answers in its place" (ADR-014).」
func (s *QNTXServer) harnessBackend(name string) (a2apb.A2AServiceClient, bool) {
	if s.pluginRegistry == nil || !s.pluginRegistry.IsReady(name) {
		return nil, false
	}
	held, registered := s.pluginRegistry.Get(name)
	if !registered {
		return nil, false
	}
	running, runs := held.(harnessRunning)
	if !runs || !running.IsHarnessProvider() {
		return nil, false
	}
	return running.A2AServiceClient(), true
}

// 「[Fable 5.2, Fable 5.1] saidInBackend says a turn to the harness plugin backend is, run as run」
// 「[Fable 5.2, Fable 5.1] says, and hands each record it prints to printed as it arrives. It answers」
// 「[Fable 5.2, Fable 5.1] the Task the turn ended as.」
func saidInBackend(ctx context.Context, backend a2apb.A2AServiceClient, says string, run any, printed func(a2asdk.Event) error) (*a2asdk.Task, error) {
	message, err := harnesswire.Said(says, run)
	if err != nil {
		return nil, err
	}
	// 「[Fable 5.2, Fable 5.1] The connection is core's own to the plugin, so the interface names no」
	// 「[Fable 5.2, Fable 5.1] address: the transport is the client core already holds.」
	client, err := a2aclient.NewFromEndpoints(ctx,
		[]*a2asdk.AgentInterface{a2asdk.NewAgentInterface("plugin", a2asdk.TransportProtocolGRPC)},
		a2aclient.WithTransport(a2asdk.TransportProtocolGRPC, a2aclient.TransportFactoryFn(
			func(context.Context, *a2asdk.AgentCard, *a2asdk.AgentInterface) (a2aclient.Transport, error) {
				return a2agrpc.NewGRPCTransportFromClient(backend), nil
			})))
	if err != nil {
		return nil, errors.Wrap(err, "no A2A client over the harness plugin's connection")
	}
	defer func() { _ = client.Destroy() }()

	// 「[Fable 5.2, Fable 5.1] a2a-go runs a turn apart from the call that asked for it, so a turn」
	// 「[Fable 5.2, Fable 5.1] core gives up (the node stopping, a record it could not take, a stream」
	// 「[Fable 5.2, Fable 5.1] that broke) is canceled by name: the harness stops, and writes no more」
	// 「[Fable 5.2, Fable 5.1] into a session core holds to be over.」
	var ended *a2asdk.Task
	var running a2asdk.TaskID
	defer func() {
		if running == "" || (ended != nil && ended.Status.State.Terminal()) {
			return
		}
		giving, done := context.WithTimeout(context.WithoutCancel(ctx), turnGivenUpWithin)
		defer done()
		_, _ = client.CancelTask(giving, &a2asdk.CancelTaskRequest{ID: running})
	}()
	for event, err := range client.SendStreamingMessage(ctx, &a2asdk.SendMessageRequest{Message: message}) {
		if err != nil {
			return nil, err
		}
		if running == "" {
			running = event.TaskInfo().TaskID
		}
		if task, isTask := event.(*a2asdk.Task); isTask {
			ended = task
			continue
		}
		if update, isUpdate := event.(*a2asdk.TaskStatusUpdateEvent); isUpdate && update.Status.State.Terminal() {
			ended = &a2asdk.Task{ID: update.TaskID, ContextID: update.ContextID, Status: update.Status}
			continue
		}
		if err := printed(event); err != nil {
			return nil, err
		}
	}
	if ended == nil || !ended.Status.State.Terminal() {
		return nil, errors.New("the harness plugin's stream ended before its turn did")
	}
	return ended, nil
}
