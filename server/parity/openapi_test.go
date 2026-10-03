package parity

import (
	"slices"
	"strings"
	"testing"
)

// An OpenAPI description's shape: an operation of each form the pinned GitHub
// description writes.
const openAPIShapes = `{
  "openapi": "3.0.3",
  "paths": {
    "/repos/{owner}/issues": {
      "get": {
        "summary": "List issues",
        "description": "Lists issues.",
        "parameters": [
          {"name": "owner", "in": "path", "required": true, "description": "The account owner.", "schema": {"type": "string"}},
          {"name": "per_page", "in": "query", "schema": {"type": "integer"}},
          {"name": "accept", "in": "header", "schema": {"type": "string"}}
        ],
        "responses": {"200": {"content": {"application/json": {"schema": {"type": "array", "items": {"$ref": "#/components/schemas/issue"}}}}}}
      },
      "post": {
        "summary": "Create an issue",
        "requestBody": {"content": {"application/json": {"schema": {
          "type": "object", "required": ["title"],
          "properties": {
            "title": {"type": "string", "description": "The title of the issue."},
            "fields": {"type": "array", "items": {"type": "object", "properties": {"field_id": {"type": "integer"}}}}
          }
        }}}},
        "responses": {"201": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/issue"}}}}}
      },
      "delete": {"summary": "Lock", "responses": {"204": {"description": "No content"}}}
    }
  },
  "components": {"schemas": {
    "simple-user": {"title": "Simple User", "type": "object", "properties": {"login": {"type": "string"}}},
    "issue": {
      "title": "Issue",
      "description": "Issues are a great way to keep track of tasks.",
      "type": "object",
      "required": ["title"],
      "properties": {
        "title": {"type": "string"},
        "user": {"allOf": [{"$ref": "#/components/schemas/simple-user"}], "nullable": true},
        "pull_request": {"type": "object", "properties": {"url": {"type": "string"}}}
      }
    },
    "issue-with-more": {"allOf": [{"$ref": "#/components/schemas/issue"}, {"type": "object", "properties": {"more": {"type": "boolean"}}}]}
  }}
}`

func readOpenAPI(t *testing.T) Schema {
	t.Helper()
	schema, err := ParseOpenAPI("openapi.json", []byte(openAPIShapes))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// Each operation is a model named as our .proto names it, its columns its path
// and query parameters and the properties of its JSON body; a schema with
// properties is one, and so is each object written inline, named for where it
// is, after the model it is written in.
func TestParseOpenAPI_Models(t *testing.T) {
	schema := readOpenAPI(t)
	var names []string
	for _, m := range schema.Models {
		names = append(names, m.Name)
	}
	want := []string{"simple-user", "issue", "issue.pull_request", "issue-with-more",
		"DELETE /repos/{owner}/issues", "GET /repos/{owner}/issues", "POST /repos/{owner}/issues", "POST /repos/{owner}/issues.fields"}
	if !slices.Equal(names, want) {
		t.Errorf("models are %v, want %v", names, want)
	}

	list := modelNamed(t, schema, "GET /repos/{owner}/issues")
	if list.Says != "List issues\n\nLists issues." {
		t.Errorf("the operation says %q", list.Says)
	}
	var columns []string
	for _, c := range list.Columns {
		columns = append(columns, c.Name)
	}
	if !slices.Equal(columns, []string{"owner", "per_page"}) {
		t.Errorf("GET's columns are %v: a header is not a column", columns)
	}
	if !list.Columns[0].Required || list.Columns[0].Says != "The account owner." {
		t.Errorf("owner is %+v", list.Columns[0])
	}

	create := modelNamed(t, schema, "POST /repos/{owner}/issues")
	if len(create.Columns) != 2 || !create.Columns[0].Required || create.Columns[0].Says != "The title of the issue." {
		t.Errorf("POST's body is %+v", create.Columns)
	}
	if c := create.Columns[1]; !c.List || c.refers != "POST /repos/{owner}/issues.fields" {
		t.Errorf("fields, a list of objects written inline, is %+v", c)
	}
	if issue := modelNamed(t, schema, "issue"); issue.Says != "Issues are a great way to keep track of tasks." {
		t.Errorf("issue says %q", issue.Says)
	}
	if user := modelNamed(t, schema, "simple-user"); user.Says != "Simple User" {
		t.Errorf("a schema that says nothing else is called by its title: %q", user.Says)
	}
}

// A value that is one schema refers to its model, through allOf too; a schema
// that is all of another and more has the columns of both.
func TestParseOpenAPI_Refers(t *testing.T) {
	schema := readOpenAPI(t)
	issue := modelNamed(t, schema, "issue")
	refers := map[string]string{}
	for _, c := range issue.Columns {
		refers[c.Name] = c.refers
	}
	if refers["user"] != "simple-user" || refers["pull_request"] != "issue.pull_request" || refers["title"] != "" {
		t.Errorf("issue refers %v", refers)
	}
	var columns []string
	for _, c := range modelNamed(t, schema, "issue-with-more").Columns {
		columns = append(columns, c.Name)
	}
	if !slices.Equal(columns, []string{"title", "user", "pull_request", "more"}) {
		t.Errorf("issue-with-more's columns are %v", columns)
	}
}

// What an operation answers with is the model its 2xx JSON body is, or holds a
// list of; one that answers with no body answers nothing.
func TestParseOpenAPI_Answers(t *testing.T) {
	schema := readOpenAPI(t)
	want := map[string]Answers{
		"GET /repos/{owner}/issues":  {Model: "issue", List: true},
		"POST /repos/{owner}/issues": {Model: "issue"},
	}
	if len(schema.answers) != len(want) {
		t.Errorf("answers are %v", schema.answers)
	}
	for op, answer := range want {
		if schema.answers[op] != answer {
			t.Errorf("%s answers %+v, want %+v", op, schema.answers[op], answer)
		}
	}
}

// A comment names an operation by its method and path, and only then.
func TestOperationOf(t *testing.T) {
	for says, want := range map[string]bool{
		"GET /issues": true,
		"GET /orgs/{org}/issues Response: same response schema as List issues assigned to the authenticated user.": true,
		"Issue":            false,
		"GET the issues":   false,
		"FETCH /issues":    false,
		"All of Issue and": false,
	} {
		if _, ok := operationOf(says); ok != want {
			t.Errorf("%q names an operation: %v, want %v", says, ok, want)
		}
	}
}

// The github reference as pinned: each GitHubService request message follows
// the operation it names, field by field, what it answers with follows the
// model the operation answers with, and namespace, success and error are the
// service's own.
func TestOperationFollows_GitHub(t *testing.T) {
	schema, refused := Reference("github")
	if refused != nil {
		t.Fatal(refused.GetSays())
	}
	follows, err := OperationFollows("github", schema, "github_", "namespace", "success", "error")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string][]string{}
	for _, c := range follows.GetColumns() {
		declared[c.GetField()] = append(declared[c.GetField()], c.GetColumn())
	}
	for field, column := range map[string]string{
		"protocol.GitHubListIssuesAssignedToTheAuthenticatedUserRequest.filter": "GET /issues.filter",
		"protocol.GitHubIssuesIssuesIssue.title":                                "issue.title",
		"protocol.GitHubIssuesIssuesSimpleUser.login":                           "simple-user.login",
	} {
		if !slices.Contains(declared[field], column) {
			t.Errorf("%s follows %v, not %s", field, declared[field], column)
		}
	}
	for _, ours := range []string{
		"protocol.GitHubListIssuesAssignedToTheAuthenticatedUserRequest.namespace",
		"protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse.success",
		"protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse.items",
	} {
		if len(declared[ours]) > 0 {
			t.Errorf("%s is the service's own, and follows %v", ours, declared[ours])
		}
	}
	// No message of ours that is not GitHubService's names one of GitHub's
	// operations: Pulse's // GET /jobs/{job_id}/stages is its own.
	for field := range declared {
		if !strings.HasPrefix(field, "protocol.GitHub") {
			t.Errorf("%s follows github", field)
		}
	}
}
