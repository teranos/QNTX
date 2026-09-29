package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/teranos/QNTX/server"
	"github.com/teranos/errors"
)

// Umami is the reference staands follows: "Staands will be Umami, one reference
// and not a blend." Pinned to v3.3.1, at the commit that tag points to.
//
// An item is a model in Umami's Prisma schema. Its score is the share of the
// model's fields that a stand records, 0 to 100. A stand records a field when
// the Arrival that staandArrival builds carries the Arrival field mapped to it.
const umamiDir = "cmd/parity/umami_v3.3.1_ca661c7"

// Model is one Prisma model and its scalar fields. Relations are not fields of
// the record, so they are left out.
type Model struct {
	Name   string
	Fields []string
}

var prismaScalars = []string{"String", "Int", "BigInt", "Boolean", "DateTime", "Decimal", "Json", "Float", "Bytes"}

// ParsePrisma reads the models out of a schema.prisma.
func ParsePrisma(path string) ([]Model, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the Umami schema from %s", path)
	}

	var models []Model
	var cur *Model
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case cur == nil:
			if name, ok := strings.CutPrefix(line, "model "); ok {
				name, _, _ = strings.Cut(name, " ")
				models = append(models, Model{Name: name})
				cur = &models[len(models)-1]
			}
		case line == "}":
			cur = nil
		case line == "", strings.HasPrefix(line, "//"), strings.HasPrefix(line, "@@"):
		default:
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			kind := strings.TrimSuffix(strings.TrimSuffix(parts[1], "?"), "[]")
			if slices.Contains(prismaScalars, kind) {
				cur.Fields = append(cur.Fields, parts[0])
			}
		}
	}
	if len(models) == 0 {
		return nil, errors.Newf("no model found in the Umami schema at %s", path)
	}
	return models, nil
}

// umamiColumns says which Umami field each Arrival field is. Every line is the
// proto's own word for it: the visitor is "a person, durable across visits" and
// Umami carries both ids, the session for the person and the visit for the
// sitting; the market and slug together are what every other tool calls site_id.
var umamiColumns = []struct {
	Arrival string
	Columns []string
}{
	{"market", []string{"WebsiteEvent.websiteId", "Session.websiteId"}},
	{"slug", []string{"WebsiteEvent.websiteId", "Session.websiteId"}},
	{"at", []string{"WebsiteEvent.createdAt"}},
	{"visitor", []string{"WebsiteEvent.sessionId", "Session.id"}},
	{"visit", []string{"WebsiteEvent.visitId"}},
	{"path", []string{"WebsiteEvent.urlPath"}},
	{"query", []string{"WebsiteEvent.urlQuery"}},
	{"event", []string{"WebsiteEvent.eventName"}},
	{"referrer_domain", []string{"WebsiteEvent.referrerDomain"}},
	{"referrer_path", []string{"WebsiteEvent.referrerPath"}},
	{"utm_source", []string{"WebsiteEvent.utmSource"}},
	{"utm_medium", []string{"WebsiteEvent.utmMedium"}},
	{"utm_campaign", []string{"WebsiteEvent.utmCampaign"}},
	{"utm_content", []string{"WebsiteEvent.utmContent"}},
	{"utm_term", []string{"WebsiteEvent.utmTerm"}},
	{"browser", []string{"Session.browser"}},
	{"operating_system", []string{"Session.os"}},
	{"device", []string{"Session.device"}},
	{"screen", []string{"Session.screen"}},
	{"language", []string{"Session.language"}},
	{"country", []string{"Session.country"}},
	{"region", []string{"Session.region"}},
	{"city", []string{"Session.city"}},
	{"params", []string{"EventData.dataKey", "EventData.stringValue"}},
}

// umamiClades groups the models by what they are for. Prisma has no grouping of
// its own, and a model that is in none of these is an error, so a schema that
// grows cannot leave a model out of the report unseen.
var umamiClades = []struct {
	Name   string
	Models []string
}{
	{"analytics", []string{"WebsiteEvent", "EventData", "Session", "SessionData", "SessionLink", "Revenue", "HeatmapEvent", "SessionReplay", "SessionReplaySaved"}},
	{"reports", []string{"Report", "Segment"}},
	{"sites", []string{"Website", "Share", "Link", "Pixel", "Board"}},
	{"accounts", []string{"User", "Team", "TeamUser"}},
	{"two-factor", []string{"TwoFactorAuth", "TwoFactorBackupCode", "TwoFactorOtpUsed", "TwoFactorRateLimit"}},
	{"system", []string{"AppSetting"}},
}

// Item is one model and how much of it a stand records.
type Item struct {
	Name    string
	Covered int
	Total   int
}

// Score is 0 to 100 and reads 100 only when every field is covered.
func (i Item) Score() int {
	if i.Total == 0 {
		return 0
	}
	return i.Covered * 100 / i.Total
}

// Clade is a group of items.
type Clade struct {
	Name  string
	Items []Item
}

// UmamiReport scores every model in the schema against the Arrival fields a
// stand records.
func UmamiReport(models []Model, recorded []string) ([]Clade, error) {
	byName := make(map[string]Model, len(models))
	for _, m := range models {
		byName[m.Name] = m
	}

	// covered[model][field] is true once a recorded Arrival field maps to it.
	covered := map[string]map[string]bool{}
	for _, mapping := range umamiColumns {
		for _, column := range mapping.Columns {
			model, field, _ := strings.Cut(column, ".")
			m, ok := byName[model]
			if !ok || !slices.Contains(m.Fields, field) {
				return nil, errors.Newf("Arrival field %s maps to %s, which is not in the Umami schema", mapping.Arrival, column)
			}
			if !slices.Contains(recorded, mapping.Arrival) {
				continue
			}
			if covered[model] == nil {
				covered[model] = map[string]bool{}
			}
			covered[model][field] = true
		}
	}

	seen := map[string]bool{}
	var clades []Clade
	for _, def := range umamiClades {
		clade := Clade{Name: def.Name}
		for _, name := range def.Models {
			m, ok := byName[name]
			if !ok {
				return nil, errors.Newf("clade %s names model %s, which is not in the Umami schema", def.Name, name)
			}
			seen[name] = true
			clade.Items = append(clade.Items, Item{Name: name, Covered: len(covered[name]), Total: len(m.Fields)})
		}
		clades = append(clades, clade)
	}
	for _, m := range models {
		if !seen[m.Name] {
			return nil, errors.Newf("model %s is in the Umami schema and in no clade", m.Name)
		}
	}
	return clades, nil
}

func (c Clade) allAt(pred func(Item) bool) bool {
	for _, i := range c.Items {
		if !pred(i) {
			return false
		}
	}
	return true
}

// RenderUmami prints the report. A clade of nothing but zeros is one line. A
// clade with anything above zero opens and lists every member. A clade at 100
// throughout is left out unless all is set.
func RenderUmami(clades []Clade, all bool) string {
	width := 0
	for _, c := range clades {
		width = max(width, len(c.Name))
		for _, i := range c.Items {
			width = max(width, len(i.Name)+2)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", filepath.Base(umamiDir))
	hidden := 0
	for _, c := range clades {
		switch {
		case c.allAt(func(i Item) bool { return i.Covered == 0 }):
			fmt.Fprintf(&b, "  %-*s  %3d   (%d models)\n", width, c.Name, 0, len(c.Items))
		case c.allAt(func(i Item) bool { return i.Covered == i.Total }):
			if !all {
				hidden++
				continue
			}
			fmt.Fprintf(&b, "  %-*s  %3d   (%d models)\n", width, c.Name, 100, len(c.Items))
		default:
			fmt.Fprintf(&b, "  %s\n", c.Name)
			for _, i := range c.Items {
				fmt.Fprintf(&b, "    %-*s  %3d   (%d/%d)\n", width-2, i.Name, i.Score(), i.Covered, i.Total)
			}
		}
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "  %d at 100 not shown, -all shows them\n", hidden)
	}
	b.WriteString("\n")
	return b.String()
}

// Umami reads the pinned schema and what a stand records, and renders the score.
func Umami(root string, all bool) (string, error) {
	models, err := ParsePrisma(filepath.Join(root, umamiDir, "schema.prisma"))
	if err != nil {
		return "", err
	}
	recorded, err := server.StaandRecorded()
	if err != nil {
		return "", err
	}
	clades, err := UmamiReport(models, recorded)
	if err != nil {
		return "", err
	}
	return RenderUmami(clades, all), nil
}
