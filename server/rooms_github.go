package server

// "Given the Node Room"
// "When a PR comment lands on QNTX, QNTX-Web, QNTX-App"
// "It get’s said into the Room"

import (
	"context"
	"fmt"
	"time"

	"github.com/teranos/QNTX/internal/roomcomments"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
)

func (s *QNTXServer) keepRoomComment(ctx context.Context, c *roomcomments.Comment) error {
	if s.nodeDB == nil {
		return errors.New("the node has no operational store for room comments")
	}
	return (roomcomments.Outbox{DB: s.nodeDB}).Keep(ctx, c)
}

func (s *QNTXServer) nodeRoomForComments(ctx context.Context) (roomcomments.Target, error) {
	provider, rooms, refused := s.roomsOf("")
	if refused != nil {
		return roomcomments.Target{}, errors.New(refused.Says)
	}
	return nodeRoomForComments(ctx, rooms, provider)
}

func nodeRoomForComments(ctx context.Context, rooms protocol.RoomsServiceClient, provider string) (roomcomments.Target, error) {
	listed, err := rooms.ListRooms(ctx, &protocol.ListRoomsRequest{Provider: provider})
	if err != nil {
		return roomcomments.Target{}, errors.Wrap(err, "the Node Room was not found")
	}
	target := roomcomments.Target{Provider: provider}
	for _, room := range listed.GetList() {
		if room.GetName() != nodeRoomName {
			continue
		}
		if target.Room != "" {
			return roomcomments.Target{}, errors.New("the provider holds more than one Node Room")
		}
		target.Room = room.GetId()
	}
	if target.Room == "" {
		return roomcomments.Target{}, errors.New("the provider holds no Node Room")
	}
	return target, nil
}

func (s *QNTXServer) sayRoomComment(ctx context.Context, target roomcomments.Target, c roomcomments.Comment) (string, error) {
	_, rooms, refused := s.roomsOf(target.Provider)
	if refused != nil {
		return "", errors.New(refused.Says)
	}
	said, err := sayRoomComment(ctx, rooms, target, c)
	if err == nil {
		s.wakeAfterComment(target, c)
	}
	return said, err
}

func sayRoomComment(ctx context.Context, rooms protocol.RoomsServiceClient, target roomcomments.Target, c roomcomments.Comment) (string, error) {
	found, err := rooms.GetRoom(ctx, &protocol.GetRoomRequest{Provider: target.Provider, Room: target.Room})
	if err != nil {
		return "", errors.Wrap(err, "the comment's Node Room did not read")
	}
	if found.GetRoom().GetName() != nodeRoomName {
		return "", errors.New("the comment's destination is no longer the Node Room")
	}
	// It is said by whoever wrote it, as the provider registers them in the
	// room, not by the node, which did not say it. Posting comment text does
	// not ask an agent to act on it: waking one is wakeAfterComment's.
	said, err := rooms.PostToRoom(ctx, &protocol.PostToRoomRequest{
		Provider: target.Provider, Room: target.Room, By: &protocol.RoomAgent{Name: c.Poster()}, Body: c.Message(),
		Files: []*protocol.RoomFile{{Name: fmt.Sprintf("github-comment-%d.md", c.ID), Type: "text/markdown", Data: []byte(c.Body)}},
	})
	if err != nil {
		return "", errors.Wrap(err, "the PR comment was not posted")
	}
	return said.GetId(), nil
}

func (s *QNTXServer) sayWaitingRoomComments() {
	wait := time.Duration(0)
	for {
		select {
		case <-s.lifetime().Done():
			return
		case <-time.After(wait):
		}
		ctx, cancel := context.WithTimeout(s.lifetime(), 30*time.Second)
		var err error
		if s.nodeDB == nil {
			err = errors.New("the node has no operational store for room comments")
		} else {
			err = (roomcomments.Outbox{DB: s.nodeDB}).Flush(ctx, s.nodeRoomForComments, s.sayRoomComment)
		}
		cancel()
		wait = 2 * time.Second
		if err != nil {
			if s.lifetime().Err() != nil {
				return
			}
			s.logger.Errorw("A PR comment is waiting to be said into the Node Room", "error", err)
			wait = 30 * time.Second
		}
	}
}
