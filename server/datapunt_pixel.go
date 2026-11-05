package server

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
)

// "CUE allows even web Analytics Pixels to be dynamically defined"
// "CUE schema changes need approvals from User in a way that's sensible, goes
// for both Datapunt and the converted Staands"
//
// What a datapunt's snippet may say is declared in CUE, in the datapunt plugin,
// and a change to it is approved by merging it. The node asks the plugin's pixel
// sigil and keeps the answer; it declares nothing of its own.

// datapuntPlugin is the plugin that declares the pixels.
const datapuntPlugin = "datapunt"

// datapuntPixelAttr names, on a datapunt's created line, the pixel it is.
const datapuntPixelAttr = "pixel"

// datapuntRefused is the datapunt plugin's own word for a value the schema would
// not hold, so a pixel's refusals read back with its by refused.
const datapuntRefused = "datapunt:refused"

// How long the plugin's answer is kept before it is asked again. A schema
// changes only when the plugin is rebuilt, so a minute is a restart's worth.
const datapuntPixelFresh = time.Minute

// datapuntArrivalFields are what every arrival carries in its own fields, the
// names datapunt's #arrival declares. A param of one of these names is not the
// pixel's own.
var datapuntArrivalFields = []string{
	"path", "visitor", "visit", "referrer_domain", "referrer_path",
	"utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term",
}

// datapuntVocabulary is what one pixel's snippet may say: the events it may
// name, and its own fields, each with the values it may take when it is an enum
// or a bool.
type datapuntVocabulary struct {
	pixel  string
	events []string
	fields map[string]datapuntFieldRule
	asked  time.Time
}

type datapuntFieldRule struct {
	kind   string
	values []string
}

// datapuntPixelAnswer is the pixel sigil's answer, as datapunt gives it.
type datapuntPixelAnswer struct {
	Rows []struct {
		Pixel  string   `json:"pixel"`
		Events []string `json:"events"`
		Fields []struct {
			Field  string   `json:"field"`
			Type   string   `json:"type"`
			Values []string `json:"values"`
		} `json:"fields"`
	} `json:"rows"`
}

// datapuntPixelAsk asks the datapunt plugin what one pixel may say. A pixel the
// schema does not declare is an error that says so.
func (s *QNTXServer) datapuntPixelAsk(ctx context.Context, pixel string) (*datapuntVocabulary, error) {
	if s.pluginRegistry == nil {
		return nil, errors.New("no plugins are served here, so no pixel is declared")
	}
	p, ok := s.pluginRegistry.Get(datapuntPlugin)
	if !ok {
		return nil, errors.New("the datapunt plugin is not enabled, so no pixel is declared")
	}
	proxy, ok := p.(*grpcplugin.ExternalDomainProxy)
	if !ok {
		return nil, errors.New("the datapunt plugin is not a gRPC plugin")
	}
	resp, err := proxy.Client().HandleHTTP(ctx, &protocol.HTTPRequest{
		Method: "GET",
		Path:   "/pixel?pixel=" + url.QueryEscape(pixel),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "the datapunt plugin was not asked for pixel %s", pixel)
	}
	if resp.StatusCode != 200 {
		return nil, errors.Newf("the datapunt plugin declares no pixel %s: %s", pixel, string(resp.Body))
	}
	return datapuntVocabularyOf(pixel, resp.Body)
}

// datapuntVocabularyOf reads the pixel sigil's answer for one pixel.
func datapuntVocabularyOf(pixel string, body []byte) (*datapuntVocabulary, error) {
	var a datapuntPixelAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, errors.Wrapf(err, "the datapunt plugin's answer for pixel %s did not read", pixel)
	}
	for _, row := range a.Rows {
		if row.Pixel != pixel {
			continue
		}
		v := &datapuntVocabulary{pixel: pixel, events: row.Events, fields: map[string]datapuntFieldRule{}}
		for _, f := range row.Fields {
			if slices.Contains(datapuntArrivalFields, f.Field) {
				continue
			}
			v.fields[f.Field] = datapuntFieldRule{kind: f.Type, values: f.Values}
		}
		return v, nil
	}
	return nil, errors.Newf("the datapunt plugin's answer names no pixel %s", pixel)
}

// datapuntPixel is what one pixel may say, as the plugin last said it. Asked
// again once it is a minute old; while the plugin cannot be asked, the last
// answer stands. Never asked and not askable is an error.
func (s *QNTXServer) datapuntPixel(ctx context.Context, pixel string) (*datapuntVocabulary, error) {
	var held *datapuntVocabulary
	if v, ok := s.datapuntPixels.Load(pixel); ok {
		held, _ = v.(*datapuntVocabulary)
	}
	if held != nil && time.Since(held.asked) < datapuntPixelFresh {
		return held, nil
	}
	asked, err := s.datapuntPixelAsk(ctx, pixel)
	if err != nil {
		if held != nil {
			s.logger.Warnw("Datapunt pixel not asked again, the last answer stands",
				"pixel", pixel, "error", err)
			return held, nil
		}
		return nil, err
	}
	asked.asked = time.Now()
	s.datapuntPixels.Store(pixel, asked)
	return asked, nil
}

// datapuntRefusal is one thing an arrival said that its pixel would not hold,
// in the parts datapunt's own refusals carry.
type datapuntRefusal struct {
	field, value, param, says string
}

// datapuntHeld is what of an arrival its pixel holds. An event it does not
// declare refuses the arrival; a param that is not one of its fields, or a
// value its field does not take, is refused and dropped. The refusals are what
// to write down.
func datapuntHeld(v *datapuntVocabulary, a *protocol.Arrival) (bool, []datapuntRefusal) {
	var refused []datapuntRefusal
	held := true
	if !slices.Contains(v.events, a.Event) {
		held = false
		refused = append(refused, datapuntRefusal{
			field: "event", value: a.Event, param: "value",
			says: "not an event pixel " + v.pixel + " declares: " + a.Event + ". Declared: " + strings.Join(v.events, ", "),
		})
	}
	keys := make([]string, 0, len(a.Params))
	for k := range a.Params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		value := a.Params[k]
		rule, ok := v.fields[k]
		if !ok {
			refused = append(refused, datapuntRefusal{field: k, value: value, param: "field",
				says: "no such field for " + v.pixel + ": " + k})
			delete(a.Params, k)
			continue
		}
		legal := rule.values
		if rule.kind == "bool" {
			legal = []string{"true", "false"}
		}
		if (rule.kind == "enum" || rule.kind == "bool") && !slices.Contains(legal, value) {
			refused = append(refused, datapuntRefusal{field: k, value: value, param: "value",
				says: "not a legal " + k + " value: " + value + ". Legal: " + strings.Join(legal, ", ")})
			delete(a.Params, k)
		}
	}
	return held, refused
}

// datapuntRefuse writes down what a pixel would not hold, as datapunt writes its
// own refusals: about the page, in the pixel's kind, by the datapunt.
func (s *QNTXServer) datapuntRefuse(store ats.AttestationStore, v *datapuntVocabulary, slug, subject string, refused []datapuntRefusal) {
	for _, r := range refused {
		id, err := identity.GenerateASUIDWithRetry("AS", subject, datapuntRefused, v.pixel, store.AttestationExists)
		if err != nil {
			s.logger.Errorw("Datapunt refusal lost: no ASID for it", "slug", slug, "error", err)
			continue
		}
		now := time.Now()
		if err := store.CreateAttestation(&types.As{
			ID:         id,
			Subjects:   []string{subject},
			Predicates: []string{datapuntRefused},
			Contexts:   []string{v.pixel},
			Actors:     []string{datapuntPrefix + slug},
			Source:     datapuntSource,
			Timestamp:  now,
			Attributes: map[string]any{
				"field": r.field, "value": r.value, "param": r.param, "says": r.says,
				datapuntSlugAttr: slug,
			},
			CreatedAt: now,
		}); err != nil {
			s.logger.Errorw("Datapunt refusal lost: the store did not take it", "slug", slug, "error", err)
		}
	}
}
