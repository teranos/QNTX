// Package roomcomments carries PR comments into the Node Room.
//
// "When a PR comment lands on QNTX, QNTX-Web, QNTX-App"
// "It get’s said into the Room"
// "Visibly so"
package roomcomments

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
	"golang.org/x/net/html"
)

type Comment struct {
	Key    string
	Repo   string
	Number int64
	ID     int64
	Author string
	URL    string
	Body   string
	At     string
	Action string
	// Kind is the event it came in: issue_comment, pull_request_review_comment
	// or pull_request_review.
	Kind string
	Path string
	Line int64
	// App is the slug of the GitHub App the comment was written through, empty
	// for a person. GitHub sets it; a person cannot.
	App string
	// PRBody is the description of the pull request the comment is on.
	PRBody string
}

// "For example given the Qodo comments we see on many of our PR’s"
func Parse(event string, body []byte) (*Comment, error) {
	if event != "issue_comment" && event != "pull_request_review_comment" && event != "pull_request_review" && event != "pull_request" {
		return nil, nil
	}
	type words struct {
		ID        int64  `json:"id"`
		Body      string `json:"body"`
		Updated   string `json:"updated_at"`
		Created   string `json:"created_at"`
		Submitted string `json:"submitted_at"`
		Path      string `json:"path"`
		Line      int64  `json:"line"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		App struct {
			Slug string `json:"slug"`
		} `json:"performed_via_github_app"`
	}
	var sent struct {
		Action     string `json:"action"`
		Repository struct {
			Name string `json:"full_name"`
		} `json:"repository"`
		Issue struct {
			Number int64           `json:"number"`
			Body   string          `json:"body"`
			PR     json.RawMessage `json:"pull_request"`
		} `json:"issue"`
		PR struct {
			ID      int64  `json:"id"`
			Number  int64  `json:"number"`
			Body    string `json:"body"`
			Updated string `json:"updated_at"`
		} `json:"pull_request"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
		Comment words `json:"comment"`
		Review  words `json:"review"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		return nil, errors.Wrap(err, "the PR comment did not read")
	}
	repo := ""
	for _, named := range []string{"teranos/QNTX", "teranos/QNTX-Web", "teranos/QNTX-App"} {
		if strings.EqualFold(sent.Repository.Name, named) {
			repo = named
		}
	}
	if repo == "" {
		return nil, nil
	}
	if event == "pull_request" {
		return draftChange(repo, sent.Action, sent.PR.Number, sent.PR.ID, sent.PR.Updated, sent.PR.Body, sent.Sender.Login)
	}
	w, number, fragment, prBody := sent.Comment, sent.PR.Number, "discussion_r", sent.PR.Body
	if event == "issue_comment" {
		if len(sent.Issue.PR) == 0 || string(sent.Issue.PR) == "null" {
			return nil, nil
		}
		number, fragment, prBody = sent.Issue.Number, "issuecomment-", sent.Issue.Body
	}
	if event == "pull_request_review" {
		if sent.Action != "submitted" && sent.Action != "edited" {
			return nil, nil
		}
		w, fragment = sent.Review, "pullrequestreview-"
	} else if sent.Action != "created" && sent.Action != "edited" {
		return nil, nil
	}
	if strings.TrimSpace(w.Body) == "" {
		return nil, nil
	}
	if w.ID <= 0 || number <= 0 || w.User.Login == "" {
		return nil, errors.New("the PR comment needs its ID, PR number and author")
	}
	if len(w.Body) > 1<<20 {
		return nil, errors.New("the PR comment exceeds 1 MiB")
	}
	at := w.Updated
	if at == "" {
		at = w.Submitted
	}
	if at == "" {
		at = w.Created
	}
	if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
		return nil, errors.Wrap(err, "the PR comment's time did not read")
	}
	c := &Comment{Repo: repo, Number: number, ID: w.ID, Author: w.User.Login, Body: w.Body, At: at, Kind: event, Action: sent.Action, Path: w.Path, Line: w.Line, App: w.App.Slug, PRBody: prBody}
	c.URL = fmt.Sprintf("https://github.com/%s/pull/%d#%s%d", repo, number, fragment, w.ID)
	// A redelivery has the same key; an edit is a new version, including a
	// later edit restoring earlier words. Delivery IDs need not be retained.
	key, err := json.Marshal([]string{event, c.URL, at, sent.Action, w.Body})
	if err != nil {
		return nil, errors.Wrap(err, "the PR comment's identity did not encode")
	}
	digest := sha256.Sum256(key)
	c.Key = hex.EncodeToString(digest[:])
	return c, nil
}

// "Draft->Ready change that should wake. but, Ready->Draft needs not wake, but
// should still enter the room."
//
// draftChange is a pull request going from a draft to ready for review, or
// back. It has no words of its own: the room is told who changed it.
func draftChange(repo, action string, number, id int64, at, prBody, who string) (*Comment, error) {
	if action != "ready_for_review" && action != "converted_to_draft" {
		return nil, nil
	}
	if id <= 0 || number <= 0 || who == "" {
		return nil, errors.New("the pull request needs its ID, number and who changed it")
	}
	if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
		return nil, errors.Wrap(err, "the pull request's time did not read")
	}
	c := &Comment{Repo: repo, Number: number, ID: id, Author: who, At: at, Kind: "pull_request", Action: action, PRBody: prBody}
	c.URL = fmt.Sprintf("https://github.com/%s/pull/%d", repo, number)
	key, err := json.Marshal([]string{"pull_request", c.URL, at, action})
	if err != nil {
		return nil, errors.Wrap(err, "the pull request change's identity did not encode")
	}
	digest := sha256.Sum256(key)
	c.Key = hex.EncodeToString(digest[:])
	return c, nil
}

func escape(s string) string {
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`").Replace(s)
}

func (c Comment) Message() string {
	if c.Kind == "pull_request" {
		what := "marked the pull request ready for review"
		if c.Action == "converted_to_draft" {
			what = "converted the pull request to a draft"
		}
		return fmt.Sprintf("### [%s #%d](https://github.com/%s/pull/%d)\n\n**%s** %s · %s\n\n[View pull request](%s)", c.Repo, c.Number, c.Repo, c.Number, escape(c.Author), what, c.At, c.URL)
	}
	action := "commented"
	if c.Action == "edited" {
		action = "updated their comment"
	}
	header := fmt.Sprintf("### [%s #%d](https://github.com/%s/pull/%d)\n\n**%s** %s · %s\n\n", c.Repo, c.Number, c.Repo, c.Number, escape(c.Author), action, c.At)
	if c.Path != "" {
		header += fmt.Sprintf("%s · line %d\n\n", escape(c.Path), c.Line)
	}
	return header + readable(c.Body) + "\n\n[View comment on GitHub](" + c.URL + ")"
}

// The room reads Markdown. Qodo's presentation tags become text and line
// breaks; the original, unmodified body travels as a Markdown attachment.
// Fenced code and inline backticks retain their literal HTML examples.
func readable(body string) string {
	var out strings.Builder
	fence := ""
	for _, line := range strings.Split(body, "\n") {
		plain := strings.TrimLeft(line, " >\t")
		if strings.HasPrefix(plain, "```") || strings.HasPrefix(plain, "~~~") {
			mark := plain[:3]
			if fence == "" {
				fence = mark
			} else if fence == mark {
				fence = ""
			}
			out.WriteString(line + "\n")
			continue
		}
		if fence != "" {
			out.WriteString(line + "\n")
			continue
		}
		pieces := strings.Split(line, "`")
		for i, piece := range pieces {
			if i > 0 {
				out.WriteByte('`')
			}
			if i%2 == 1 {
				out.WriteString(piece)
				continue
			}
			tokens := html.NewTokenizer(strings.NewReader(piece))
			for {
				kind := tokens.Next()
				if kind == html.ErrorToken {
					if tokens.Err() != io.EOF {
						out.WriteString(piece)
					}
					break
				}
				if kind == html.TextToken {
					out.Write(tokens.Text())
					continue
				}
				if kind == html.CommentToken {
					continue
				}
				token := tokens.Token()
				switch token.Data {
				case "br", "h1", "h2", "h3", "h4", "summary", "pre", "p", "tr", "li":
					out.WriteByte('\n')
				case "details", "dl", "dd", "div", "span", "b", "i", "strong", "em", "ins", "code", "a", "table", "thead", "tbody", "td", "th", "ul", "ol", "img":
				default:
					out.WriteString(token.String())
				}
			}
		}
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}
