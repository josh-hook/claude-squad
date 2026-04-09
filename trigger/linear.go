package trigger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const linearAPIEndpoint = "https://api.linear.app/graphql"

// LinearClient is a simple GraphQL client for the Linear API.
type LinearClient struct {
	apiKey     string
	httpClient *http.Client
}

// NewLinearClient creates a new Linear API client with the given API key.
func NewLinearClient(apiKey string) *LinearClient {
	return &LinearClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Issue represents a Linear issue.
type Issue struct {
	ID          string        `json:"id"`
	Identifier  string        `json:"identifier"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	State       WorkflowState `json:"state"`
	Relations   struct {
		Nodes []IssueRelation `json:"nodes"`
	} `json:"relations"`
}

// WorkflowState represents the state of a Linear issue.
type WorkflowState struct {
	Name string `json:"name"`
	Type string `json:"type"` // backlog, unstarted, started, completed, canceled
}

// IssueRelation represents a relation between two Linear issues.
type IssueRelation struct {
	Type         string `json:"type"` // blocks, duplicate, related
	RelatedIssue Issue  `json:"relatedIssue"`
}

// IsReady returns true if the issue has no unresolved dependencies.
// An issue is ready if:
//   - it has no relations, OR
//   - all related issues are in a completed or canceled state
func (i *Issue) IsReady() bool {
	if len(i.Relations.Nodes) == 0 {
		return true
	}
	for _, rel := range i.Relations.Nodes {
		st := rel.RelatedIssue.State.Type
		if st != "completed" && st != "canceled" {
			return false
		}
	}
	return true
}

// graphQLRequest is the body of a GraphQL request.
type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// graphQLResponse is the top-level response from the Linear API.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// FetchAssignedIssues returns open issues assigned to the given email address.
// It excludes issues already in completed or canceled state.
func (c *LinearClient) FetchAssignedIssues(email string) ([]Issue, error) {
	const query = `query($email: String!) {
		issues(filter: {
			assignee: { email: { eq: $email } }
			state: { type: { nin: ["completed", "canceled"] } }
		}) {
			nodes {
				id
				identifier
				title
				description
				state {
					name
					type
				}
				relations {
					nodes {
						type
						relatedIssue {
							id
							identifier
							title
							state {
								name
								type
							}
						}
					}
				}
			}
		}
	}`

	variables := map[string]interface{}{
		"email": email,
	}

	var result struct {
		Issues struct {
			Nodes []Issue `json:"nodes"`
		} `json:"issues"`
	}

	if err := c.executeQuery(query, variables, &result); err != nil {
		return nil, fmt.Errorf("fetch assigned issues: %w", err)
	}

	return result.Issues.Nodes, nil
}

// executeQuery sends a GraphQL query to the Linear API and unmarshals the
// response data into target.
func (c *LinearClient) executeQuery(query string, variables map[string]interface{}, target interface{}) error {
	body, err := json.Marshal(graphQLRequest{
		Query:     query,
		Variables: variables,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", linearAPIEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}

	if err := json.Unmarshal(gqlResp.Data, target); err != nil {
		return fmt.Errorf("unmarshal data: %w", err)
	}

	return nil
}
