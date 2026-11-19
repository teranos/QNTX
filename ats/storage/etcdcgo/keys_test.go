//go:build cgo && rustetcd

package etcdcgo

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

// 「[Opus 5.5] openKeys is keys under a prefix no other test touches.」
func openKeys(t *testing.T) *Keys {
	t.Helper()
	prefix := fmt.Sprintf("/keys-go/%d_%d/", os.Getpid(), time.Now().UnixNano())
	keys, err := OpenKeys(endpoint(t), prefix)
	if err != nil {
		t.Fatalf("OpenKeys at %s: %v", prefix, err)
	}
	t.Cleanup(keys.Close)
	return keys
}

// 「[Opus 5.5] What is set is what the prefix holds: what is left out of it is gone.」
func TestKeysHoldWhatIsSet(t *testing.T) {
	keys := openKeys(t)
	first := map[string][]byte{"did:key:a": []byte(`{"to":"A"}`), "did:key:b": []byte(`{"to":"B"}`)}
	if err := keys.Set(first); err != nil {
		t.Fatalf("Set: %v", err)
	}
	held, err := keys.Held()
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if !reflect.DeepEqual(held, first) {
		t.Fatalf("held %q, want %q", held, first)
	}

	then := map[string][]byte{"did:key:a": []byte(`{"to":"A2"}`)}
	if err := keys.Set(then); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if held, _ = keys.Held(); !reflect.DeepEqual(held, then) {
		t.Fatalf("held %q, want %q", held, then)
	}
}

// 「[Opus 5.5] What crosses to Rust as JSON is UTF-8: a name or a value that is not
// 「[Opus 5.5] would arrive changed and be said as set, so it is refused (CodeRabbit on #1768).」」
func TestKeysRefuseWhatIsNotUTF8(t *testing.T) {
	keys := openKeys(t)
	if err := keys.Set(map[string][]byte{"a": {0xff}}); err == nil {
		t.Fatal("a value that is not UTF-8 was said as set")
	}
	if err := keys.Set(map[string][]byte{string([]byte{0xff}): []byte("{}")}); err == nil {
		t.Fatal("a name that is not UTF-8 was said as set")
	}
	if held, err := keys.Held(); err != nil || len(held) != 0 {
		t.Fatalf("held %q (%v) after two refused sets, want nothing", held, err)
	}
}

func TestKeysAtAPrefixThatIsNotOneAreRefused(t *testing.T) {
	if _, err := OpenKeys(endpoint(t), "collected"); err == nil {
		t.Fatal("OpenKeys took a prefix that does not start and end at /")
	}
}
