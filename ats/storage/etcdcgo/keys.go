//go:build cgo && rustetcd

package etcdcgo

/*
#include "etcd_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"sync"
	"unicode/utf8"
	"unsafe"

	errors "github.com/teranos/sacred-error"
)

// "「[Opus 5.5] our gap is kv」"

// 「[Opus 5.5] Keys are what the node holds as a map, kept in etcd key by key under
// 「[Opus 5.5] one prefix, so a reader watches the prefix and has it as the node does.」」
type Keys struct {
	ptr    unsafe.Pointer // *C.EtcdKeys
	prefix string
	mu     sync.Mutex
	closed bool
}

// 「[Opus 5.5] OpenKeys connects to the etcd at endpoint, for the keys under prefix,
// 「[Opus 5.5] which starts and ends at /.」」
func OpenKeys(endpoint, prefix string) (*Keys, error) {
	cEndpoint := C.CString(endpoint)
	defer C.free(unsafe.Pointer(cEndpoint))
	cPrefix := C.CString(prefix)
	defer C.free(unsafe.Pointer(cPrefix))

	result := C.etcd_keys_new(cEndpoint, cPrefix)
	defer C.etcd_keys_open_result_free(result)
	if !result.success {
		return nil, failed(result.error_msg, "failed to open the keys at %s under %s", endpoint, prefix)
	}
	return &Keys{ptr: unsafe.Pointer(result.keys), prefix: prefix}, nil
}

// 「[Opus 5.5] Close frees the Rust keys and their client. Safe to call more than once.」
func (k *Keys) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return
	}
	C.etcd_keys_free((*C.EtcdKeys)(k.ptr))
	k.closed = true
}

// 「[Opus 5.5] Set makes the prefix hold whole: what changed or is new is put, what is
// 「[Opus 5.5] not in it is deleted, and what holds the same is left.」」
func (k *Keys) Set(whole map[string][]byte) error {
	asText := make(map[string]string, len(whole))
	for name, value := range whole {
		// 「[Opus 5.5] JSON carries UTF-8 alone: anything else would arrive changed and be said as set.」
		if !utf8.ValidString(name) {
			return errors.Newf("a name under %s is not UTF-8 (%q), and etcd is handed it as JSON", k.prefix, name)
		}
		if !utf8.Valid(value) {
			return errors.Newf("what %s%s holds is not UTF-8, and etcd is handed it as JSON", k.prefix, name)
		}
		asText[name] = string(value)
	}
	body, err := json.Marshal(asText)
	if err != nil {
		return errors.Wrapf(err, "what %s holds did not marshal", k.prefix)
	}
	cJSON := C.CString(string(body))
	defer C.free(unsafe.Pointer(cJSON))

	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return errors.Newf("the keys under %s are closed", k.prefix)
	}
	result := C.etcd_keys_set((*C.EtcdKeys)(k.ptr), cJSON)
	defer C.etcd_storage_result_free(result)
	if !result.success {
		return failed(result.error_msg, "etcd did not take what %s holds", k.prefix)
	}
	return nil
}

// 「[Opus 5.5] Held is what the prefix holds, each by its name under the prefix.」
func (k *Keys) Held() (map[string][]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil, errors.Newf("the keys under %s are closed", k.prefix)
	}
	result := C.etcd_keys_held((*C.EtcdKeys)(k.ptr))
	defer C.etcd_held_result_free(result)
	if !result.success {
		return nil, failed(result.error_msg, "what %s holds was not read", k.prefix)
	}
	var asText map[string]string
	if err := json.Unmarshal([]byte(C.GoString(result.json)), &asText); err != nil {
		return nil, errors.Wrapf(err, "what %s holds is not JSON", k.prefix)
	}
	held := make(map[string][]byte, len(asText))
	for name, value := range asText {
		held[name] = []byte(value)
	}
	return held, nil
}

// "LAYE OWNS IDENTITY"

// 「[Opus 5.5] Kept is what one whole key holds, and the revision it was last put at.」
type Kept struct {
	Value    []byte
	Revision int64
}

// 「[Opus 5.5] Read is what each of keys holds, each a whole key starting at /. A key
// 「[Opus 5.5] etcd does not hold is left out.」」
func (k *Keys) Read(keys ...string) (map[string]Kept, error) {
	body, err := json.Marshal(keys)
	if err != nil {
		return nil, errors.Wrap(err, "the keys to read did not marshal")
	}
	cJSON := C.CString(string(body))
	defer C.free(unsafe.Pointer(cJSON))

	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil, errors.Newf("the keys under %s are closed", k.prefix)
	}
	result := C.etcd_keys_read((*C.EtcdKeys)(k.ptr), cJSON)
	defer C.etcd_held_result_free(result)
	if !result.success {
		return nil, failed(result.error_msg, "%v were not read", keys)
	}
	var read map[string]struct {
		Value    string `json:"value"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal([]byte(C.GoString(result.json)), &read); err != nil {
		return nil, errors.Wrapf(err, "what %v hold is not JSON", keys)
	}
	kept := make(map[string]Kept, len(read))
	for key, held := range read {
		kept[key] = Kept{Value: []byte(held.Value), Revision: held.Revision}
	}
	return kept, nil
}

// 「[Opus 5.5] Swap puts put and deletes del only while each key in unchanged is at the
// 「[Opus 5.5] revision named, 0 for a key etcd does not hold; each a whole key. False when
// 「[Opus 5.5] one moved, and then nothing was written.」」
func (k *Keys) Swap(unchanged map[string]int64, put map[string][]byte, del []string) (bool, error) {
	asText := make(map[string]string, len(put))
	for key, value := range put {
		if !utf8.Valid(value) {
			return false, errors.Newf("what %s is to hold is not UTF-8, and etcd is handed it as JSON", key)
		}
		asText[key] = string(value)
	}
	body, err := json.Marshal(map[string]any{"unchanged": unchanged, "put": asText, "delete": del})
	if err != nil {
		return false, errors.Wrap(err, "the swap did not marshal")
	}
	cJSON := C.CString(string(body))
	defer C.free(unsafe.Pointer(cJSON))

	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return false, errors.Newf("the keys under %s are closed", k.prefix)
	}
	result := C.etcd_keys_swap((*C.EtcdKeys)(k.ptr), cJSON)
	defer C.etcd_held_result_free(result)
	if !result.success {
		return false, failed(result.error_msg, "etcd did not answer the swap")
	}
	var answered struct {
		Swapped bool `json:"swapped"`
	}
	if err := json.Unmarshal([]byte(C.GoString(result.json)), &answered); err != nil {
		return false, errors.Wrap(err, "etcd's answer to the swap is not JSON")
	}
	return answered.Swapped, nil
}
