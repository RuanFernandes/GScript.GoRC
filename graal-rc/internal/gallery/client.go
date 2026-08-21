// Package gallery provides the small trusted HTTP client used by the RC's
// Script Gallery bindings. Passwords and tokens never cross the Wails
// boundary's frontend directly.
package gallery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://nullborne.com"
	maxResponseBytes = 8 * 1024 * 1024
)

// Identity is the non-secret identity captured from the authenticated RC
// session. CommunityName is preferred by the API; AccountName is only a
// fallback when the server did not expose a community name.
type Identity struct {
	CommunityName string
	AccountName   string
}

func (i Identity) Username() string {
	if community := strings.TrimSpace(i.CommunityName); community != "" {
		return community
	}
	return strings.TrimSpace(i.AccountName)
}

type User struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type AuthSession struct {
	Token     string `json:"token"`
	Subject   string `json:"subject"`
	User      User   `json:"user"`
	ExpiresAt int64  `json:"expiresAt"`
}

type Script struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Content   string `json:"content,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type Project struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Visibility    string   `json:"visibility"`
	Owner         string   `json:"owner"`
	Owned         bool     `json:"owned"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
	WeaponScripts []Script `json:"weaponScripts"`
	ClassScripts  []Script `json:"classScripts"`
	NPCScripts    []Script `json:"npcScripts"`
}

type scriptResponse struct {
	Script Script `json:"script"`
}

type projectResponse struct {
	Project Project `json:"project"`
}

type scriptDetailResponse struct {
	Project Project `json:"project"`
	Script  Script  `json:"script"`
}

type projectsResponse struct {
	Projects []Project `json:"projects"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Client talks only to the trusted nullborne.com API. The explicit host and
// redirect checks prevent a compromised API response from turning the RC into
// a credential or script exfiltration client.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

func NewClient() (*Client, error) {
	baseURL, err := url.Parse(defaultBaseURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(request *http.Request, _ []*http.Request) error {
				if !isTrustedURL(request.URL) {
					return errors.New("script gallery redirect left https://nullborne.com")
				}
				return nil
			},
		},
	}, nil
}

func (c *Client) Register(ctx context.Context, identity Identity, password string) (AuthSession, error) {
	return c.authenticate(ctx, "/api/gallery/auth/register", identity, password, "register")
}

func (c *Client) Login(ctx context.Context, identity Identity, password string) (AuthSession, error) {
	return c.authenticate(ctx, "/api/gallery/auth/login", identity, password, "login")
}

func (c *Client) authenticate(ctx context.Context, route string, identity Identity, password, action string) (AuthSession, error) {
	var session AuthSession
	err := c.do(ctx, http.MethodPost, route, "", map[string]string{
		"username": identity.Username(),
		"password": password,
	}, &session)
	if err != nil {
		return AuthSession{}, fmt.Errorf("Script Gallery %s: %w", action, err)
	}
	if strings.TrimSpace(session.Token) == "" || strings.TrimSpace(session.Subject) == "" || strings.TrimSpace(session.User.Username) == "" {
		return AuthSession{}, errors.New("Script Gallery returned an incomplete authenticated session")
	}
	return session, nil
}

func (c *Client) Logout(ctx context.Context, token string) error {
	if err := c.do(ctx, http.MethodPost, "/api/gallery/auth/logout", token, nil, nil); err != nil {
		return fmt.Errorf("Script Gallery logout: %w", err)
	}
	return nil
}

func (c *Client) ListProjects(ctx context.Context, token, scriptType, query string) ([]Project, error) {
	endpoint := "/api/gallery/projects"
	values := url.Values{}
	if strings.TrimSpace(scriptType) != "" {
		values.Set("type", scriptType)
	}
	if strings.TrimSpace(query) != "" {
		values.Set("q", query)
	}
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	var response projectsResponse
	if err := c.do(ctx, http.MethodGet, endpoint, token, nil, &response); err != nil {
		return nil, fmt.Errorf("list Script Gallery projects: %w", err)
	}
	return response.Projects, nil
}

func (c *Client) CreateProject(ctx context.Context, token, name, description, visibility string) (Project, error) {
	var response projectResponse
	if err := c.do(ctx, http.MethodPost, "/api/gallery/projects", token, map[string]string{
		"name":        name,
		"description": description,
		"visibility":  visibility,
	}, &response); err != nil {
		return Project{}, fmt.Errorf("create Script Gallery project: %w", err)
	}
	return response.Project, nil
}

func (c *Client) UpdateProject(ctx context.Context, token, projectID, name, description, visibility string) (Project, error) {
	var response projectResponse
	if err := c.do(ctx, http.MethodPatch, "/api/gallery/projects/"+url.PathEscape(projectID), token, map[string]string{
		"name":        name,
		"description": description,
		"visibility":  visibility,
	}, &response); err != nil {
		return Project{}, fmt.Errorf("update Script Gallery project: %w", err)
	}
	return response.Project, nil
}

func (c *Client) DeleteProject(ctx context.Context, token, projectID string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/gallery/projects/"+url.PathEscape(projectID), token, nil, nil); err != nil {
		return fmt.Errorf("delete Script Gallery project: %w", err)
	}
	return nil
}

func (c *Client) GetScript(ctx context.Context, token, scriptID string) (Script, error) {
	var response scriptDetailResponse
	if err := c.do(ctx, http.MethodGet, "/api/gallery/scripts/"+url.PathEscape(scriptID), token, nil, &response); err != nil {
		return Script{}, fmt.Errorf("get Script Gallery script: %w", err)
	}
	return response.Script, nil
}

func (c *Client) AddScript(ctx context.Context, token, projectID, scriptType, name, content string) (Script, error) {
	var response scriptResponse
	if err := c.do(ctx, http.MethodPost, "/api/gallery/projects/"+url.PathEscape(projectID)+"/scripts", token, map[string]string{
		"type":    scriptType,
		"name":    name,
		"content": content,
	}, &response); err != nil {
		return Script{}, fmt.Errorf("upload Script Gallery script: %w", err)
	}
	return response.Script, nil
}

func (c *Client) UpdateScript(ctx context.Context, token, scriptID, name, content string) (Script, error) {
	var response scriptResponse
	if err := c.do(ctx, http.MethodPatch, "/api/gallery/scripts/"+url.PathEscape(scriptID), token, map[string]string{
		"name":    name,
		"content": content,
	}, &response); err != nil {
		return Script{}, fmt.Errorf("update Script Gallery script: %w", err)
	}
	return response.Script, nil
}

func (c *Client) DeleteScript(ctx context.Context, token, scriptID string) error {
	if err := c.do(ctx, http.MethodDelete, "/api/gallery/scripts/"+url.PathEscape(scriptID), token, nil, nil); err != nil {
		return fmt.Errorf("delete Script Gallery script: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, route, token string, payload any, target any) error {
	if c == nil || c.baseURL == nil || c.httpClient == nil {
		return errors.New("Script Gallery client is unavailable")
	}
	routeURL, err := url.Parse(route)
	if err != nil {
		return fmt.Errorf("parse Script Gallery route: %w", err)
	}
	if routeURL.IsAbs() || routeURL.Host != "" || routeURL.Fragment != "" || routeURL.Path == "" || !strings.HasPrefix(routeURL.Path, "/") {
		return errors.New("invalid Script Gallery route")
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + routeURL.Path
	if routeURL.RawPath != "" {
		endpoint.RawPath = strings.TrimRight(endpoint.RawPath, "/") + routeURL.RawPath
	} else {
		endpoint.RawPath = ""
	}
	endpoint.RawQuery = routeURL.RawQuery
	endpoint.Fragment = ""

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return readAPIError(response)
	}
	if target == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func readAPIError(response *http.Response) error {
	var body apiError
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&body); err == nil && strings.TrimSpace(body.Message) != "" {
		if body.Code != "" {
			return fmt.Errorf("API error %s (HTTP %d): %s", body.Code, response.StatusCode, body.Message)
		}
		return fmt.Errorf("API error (HTTP %d): %s", response.StatusCode, body.Message)
	}
	return fmt.Errorf("API returned HTTP %d", response.StatusCode)
}

func isTrustedURL(value *url.URL) bool {
	return value != nil && value.Scheme == "https" && strings.EqualFold(value.Hostname(), "nullborne.com") && value.User == nil
}
