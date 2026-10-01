package qntxinbox

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// bucket is the S3 bucket SES stores into.
type bucket struct{ objects map[string][]byte }

func (b *bucket) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range b.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (b *bucket) Get(_ context.Context, key string) ([]byte, error) { return b.objects[key], nil }

func (b *bucket) Move(_ context.Context, from, to string) error {
	b.objects[to] = b.objects[from]
	delete(b.objects, from)
	return nil
}

func stored(to, spam, virus string) []byte {
	return raw(
		"Received: from x by inbound-smtp.eu-central-1.amazonaws.com with SMTP id abc for "+to+"; Thu, 01 Oct 2026 21:00:00 +0000",
		"From: Ada <ada@example.com>",
		"To: "+to,
		"Subject: Keys",
		"Message-ID: <one@example.com>",
		"X-SES-Spam-Verdict: "+spam,
		"X-SES-Virus-Verdict: "+virus,
		"",
		"Under the mat.",
	)
}

func receiving(st *heldStore, objects map[string][]byte) (*Plugin, *bucket) {
	p := pluginWith(st)
	b := &bucket{objects: objects}
	p.bag = b
	return p, b
}

// Mail to a held address is filed under its User, in the inbox, and moved to filed/.
func TestReceivedMailIsFiledUnderTheUserHoldingTheAddress(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	p, b := receiving(st, map[string][]byte{"inbound/m1": stored("timothy@example.com", "PASS", "PASS")})

	got, err := p.receive(context.Background(), st)
	require.NoError(t, err)
	assert.Equal(t, tally{Filed: 1}, got)

	filed := st.wrote(PredicateMailReceived)
	require.Len(t, filed, 1)
	assert.Equal(t, []string{"m1"}, filed[0].Subjects)
	assert.Equal(t, []string{"timothy@example.com"}, filed[0].Contexts)
	assert.Equal(t, MailboxInbox, filed[0].Attributes["mailbox"])
	assert.Equal(t, "US-TIM-7K4M3B9X", filed[0].Attributes["user"])
	assert.Equal(t, "Under the mat.", filed[0].Attributes["text"])
	assert.Equal(t, "Keys", filed[0].Attributes["subject"])
	assert.Contains(t, b.objects, "filed/m1")
	assert.NotContains(t, b.objects, "inbound/m1")
}

// "Their Spam verdict"
func TestSpamIsFiledAsJunk(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	p, _ := receiving(st, map[string][]byte{"inbound/m1": stored("timothy@example.com", "FAIL", "PASS")})

	_, err := p.receive(context.Background(), st)
	require.NoError(t, err)
	filed := st.wrote(PredicateMailReceived)
	require.Len(t, filed, 1)
	assert.Equal(t, MailboxJunk, filed[0].Attributes["mailbox"])
}

// A virus is dropped, and the drop is attested.
func TestAVirusIsDroppedAndTheDropAttested(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	p, _ := receiving(st, map[string][]byte{"inbound/m1": stored("timothy@example.com", "PASS", "FAIL")})

	got, err := p.receive(context.Background(), st)
	require.NoError(t, err)
	assert.Equal(t, tally{Dropped: 1}, got)
	assert.Empty(t, st.wrote(PredicateMailReceived))
	dropped := st.wrote(PredicateMailDropped)
	require.Len(t, dropped, 1)
	assert.Nil(t, dropped[0].Attributes["text"], "a dropped mail's text was kept")
}

// Mail to an address nobody holds is not filed.
func TestMailToAnAddressNobodyHoldsIsNotFiled(t *testing.T) {
	st := &heldStore{}
	p, b := receiving(st, map[string][]byte{"inbound/m1": stored("nobody00@example.com", "PASS", "PASS")})

	got, err := p.receive(context.Background(), st)
	require.NoError(t, err)
	assert.Equal(t, tally{Unfiled: 1}, got)
	assert.Empty(t, st.written)
	assert.Contains(t, b.objects, "unfiled/m1")
}

// A message filed once is not filed again when its move did not happen.
func TestReceivingTwiceFilesOnce(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	message := stored("timothy@example.com", "PASS", "PASS")
	p, b := receiving(st, map[string][]byte{"inbound/m1": message})

	_, err := p.receive(context.Background(), st)
	require.NoError(t, err)
	b.objects["inbound/m1"] = message
	_, err = p.receive(context.Background(), st)
	require.NoError(t, err)
	assert.Len(t, st.wrote(PredicateMailReceived), 1)
}

func TestReceivingIsScheduled(t *testing.T) {
	p := NewPlugin()
	assert.Equal(t, []string{handlerReceive}, p.GetHandlerNames())
	schedules := p.GetSchedules()
	require.Len(t, schedules, 1)
	assert.Equal(t, handlerReceive, schedules[0].GetHandlerName())
	assert.True(t, schedules[0].GetEnabledByDefault())
	assert.Positive(t, schedules[0].GetIntervalSeconds())
	var _ []*protocol.ScheduleInfo = schedules
}
