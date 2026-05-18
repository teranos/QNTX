package qntxatproto

import "github.com/teranos/QNTX/ats/types"

// TimelinePost type definition for posts appearing in timeline
var TimelinePost = types.TypeDef{
	Name:             "timeline-post",
	Label:            "Timeline Post",
	Color:            "#1da1f2", // Bluesky blue
	RichStringFields: []string{"text"},
}
