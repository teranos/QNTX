package server

import (
	"context"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// "yeah, because right now, i dont even see the outgoing towards node agent in ag right? yeah thats fucked up as well"
//
// What was said to the Node Agent waits at laye, beside everything else that
// waits on its person, in ag and on laye's island. Released there, or due,
// laye hands it here, and it is the Node Agent's turn: said over the node's
// own A2A, as what was collected here was (collected.go, deliverCollected).

// layeDelivered runs what laye hands for the Node Agent. It answers the Task
// the turn is.
func (s *QNTXServer) layeDelivered(ctx context.Context, delivered auth.LayeDelivered) (string, error) {
	if s.nodeAgent == nil || delivered.DID != s.nodeAgent.did {
		return "", errors.Newf("%s is not this node's agent, and laye hands only what is the node's", delivered.DID)
	}
	as := auth.Admitted(auth.LevelRoot)
	said := context.WithValue(auth.WithAdmission(ctx, as), deliversCollected{}, true)
	client, _, refused := s.ownClient(said, "")
	if refused != nil {
		return "", errors.Newf("what laye released for %s was not said to it: %s", delivered.To, refused.GetSays())
	}
	defer func() { _ = client.Destroy() }()
	message := a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart(delivered.Says))
	answered, err := client.SendMessage(said, &a2asdk.SendMessageRequest{Message: message, Config: &a2asdk.SendMessageConfig{ReturnImmediately: true}})
	if err != nil {
		return "", errors.Wrapf(err, "what laye released for %s was not said to it", delivered.To)
	}
	task := ""
	if info := answered.TaskInfo(); info.TaskID != "" {
		task = string(info.TaskID)
	}
	// Said, not done: whether the turn ran is its Task's state, not this line.
	s.logger.Infow("What laye released for the Node Agent is said to it, as a Task", "to", delivered.To, "by", delivered.From, "task", task)
	return task, nil
}

// nodesPerson is who sees what waits for the Node Agent at laye: the first
// identity am.toml names ROOT.
func (s *QNTXServer) nodesPerson() string {
	if s.authHandler == nil {
		return ""
	}
	if roots := s.authHandler.Roots(); len(roots) > 0 {
		return roots[0]
	}
	return ""
}

// collectGitHubs collects what GitHub said of a pull request for the agent
// to: at laye where am.toml names one, quiet, beside what else waits on its
// person; else here.
func (s *QNTXServer) collectGitHubs(to collectedFor, says string) error {
	if s.layeOrigin() == "" {
		_, err := s.collect(to, collectedSaid{From: "GitHub", Says: says, At: time.Now(), Quiet: true})
		return err
	}
	own := ""
	if s.authHandler != nil {
		own, _ = s.authHandler.OwnDID()
	}
	_, err := s.handToLayeAs(s.ctx, layeSaid{To: to.Called, ToDID: to.DID, FromDID: own, From: "GitHub", Says: says, Quiet: true})
	return err
}
