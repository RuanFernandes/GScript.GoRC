package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"graal-rc/internal/connection"
	gallerylib "graal-rc/internal/gallery"
)

// ScriptGalleryIdentity is derived from the authenticated server session. The
// community name is preferred and the account name is only the fallback.
type ScriptGalleryIdentity struct {
	Username      string `json:"username"`
	CommunityName string `json:"communityName"`
	AccountName   string `json:"accountName"`
}

type ScriptGalleryUser struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type ScriptGalleryAuthState struct {
	Identity      ScriptGalleryIdentity `json:"identity"`
	Authenticated bool                  `json:"authenticated"`
	Username      string                `json:"username"`
	DisplayName   string                `json:"displayName"`
}

// ScriptGalleryScript is the Wails-safe projection of a gallery script. The
// content field is populated only when the editor requests a specific script.
type ScriptGalleryScript struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ScriptGalleryProject mirrors the API's project shape so one project can
// contain any number of weapons, classes, and NPC scripts independently.
type ScriptGalleryProject struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Visibility    string                `json:"visibility"`
	Owner         string                `json:"owner"`
	Owned         bool                  `json:"owned"`
	CreatedAt     string                `json:"createdAt"`
	UpdatedAt     string                `json:"updatedAt"`
	WeaponScripts []ScriptGalleryScript `json:"weaponScripts"`
	ClassScripts  []ScriptGalleryScript `json:"classScripts"`
	NPCScripts    []ScriptGalleryScript `json:"npcScripts"`
}

func (a *App) GetScriptGalleryAuthState() (ScriptGalleryAuthState, error) {
	identity, subject, status, err := a.currentGalleryIdentity()
	if err != nil {
		return ScriptGalleryAuthState{}, err
	}
	_, token, err := a.galleryClientAndOptionalToken(status, subject)
	if err != nil {
		return ScriptGalleryAuthState{}, err
	}

	state := ScriptGalleryAuthState{
		Identity: ScriptGalleryIdentity{
			Username:      identity.Username(),
			CommunityName: identity.CommunityName,
			AccountName:   identity.AccountName,
		},
		Authenticated: token != "",
		Username:      identity.Username(),
	}
	a.galleryMu.Lock()
	if token != "" {
		state.Username = a.galleryUser.Username
		state.DisplayName = a.galleryUser.DisplayName
	}
	a.galleryMu.Unlock()
	return state, nil
}

func (a *App) RegisterScriptGallery(password string) (ScriptGalleryAuthState, error) {
	return a.authenticateScriptGallery(password, true)
}

func (a *App) LoginScriptGallery(password string) (ScriptGalleryAuthState, error) {
	return a.authenticateScriptGallery(password, false)
}

func (a *App) LogoutScriptGallery() error {
	status := a.sessions.Status()
	_, subject, identityErr := galleryIdentityFromStatus(status)
	var logoutErr error
	client, token, clientErr := a.galleryClientAndOptionalToken(status, subject)
	if clientErr == nil && token != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		logoutErr = client.Logout(ctx, token)
		cancel()
	}
	if a.gallerySessionStore != nil && identityErr == nil {
		if err := a.gallerySessionStore.Delete(galleryAccountKey(status)); logoutErr == nil {
			logoutErr = err
		}
	}
	a.clearGallerySession()
	if identityErr != nil && clientErr != nil {
		return nil
	}
	return logoutErr
}

func (a *App) GetScriptGalleryProjects(scriptType, query string) ([]ScriptGalleryProject, error) {
	status := a.sessions.Status()
	_, subject, err := galleryIdentityFromStatus(status)
	if err != nil {
		return nil, err
	}
	client, token, err := a.galleryClientAndOptionalToken(status, subject)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	projects, err := client.ListProjects(ctx, token, scriptType, query)
	if err != nil {
		return nil, err
	}
	result := make([]ScriptGalleryProject, 0, len(projects))
	for _, project := range projects {
		result = append(result, mapGalleryProject(project))
	}
	return result, nil
}

func (a *App) CreateScriptGalleryProject(name, description, visibility string) (ScriptGalleryProject, error) {
	client, token, err := a.galleryClientAndToken()
	if err != nil {
		return ScriptGalleryProject{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	project, err := client.CreateProject(ctx, token, name, description, visibility)
	if err != nil {
		return ScriptGalleryProject{}, err
	}
	return mapGalleryProject(project), nil
}

func (a *App) UpdateScriptGalleryProject(id, name, description, visibility string) (ScriptGalleryProject, error) {
	client, token, err := a.galleryClientAndToken()
	if err != nil {
		return ScriptGalleryProject{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	project, err := client.UpdateProject(ctx, token, id, name, description, visibility)
	if err != nil {
		return ScriptGalleryProject{}, err
	}
	return mapGalleryProject(project), nil
}

func (a *App) DeleteScriptGalleryProject(id string) error {
	client, token, err := a.galleryClientAndToken()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return client.DeleteProject(ctx, token, id)
}

func (a *App) GetScriptGalleryScript(id string) (ScriptGalleryScript, error) {
	status := a.sessions.Status()
	_, subject, err := galleryIdentityFromStatus(status)
	if err != nil {
		return ScriptGalleryScript{}, err
	}
	client, token, err := a.galleryClientAndOptionalToken(status, subject)
	if err != nil {
		return ScriptGalleryScript{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	script, err := client.GetScript(ctx, token, id)
	if err != nil {
		return ScriptGalleryScript{}, err
	}
	return mapGalleryScript(script), nil
}

func (a *App) UploadScriptGalleryScript(projectID, scriptType, name, content string) (ScriptGalleryScript, error) {
	client, token, err := a.galleryClientAndToken()
	if err != nil {
		return ScriptGalleryScript{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	script, err := client.AddScript(ctx, token, projectID, scriptType, name, content)
	if err != nil {
		return ScriptGalleryScript{}, err
	}
	return mapGalleryScript(script), nil
}

func (a *App) UpdateScriptGalleryScript(id, name, content string) (ScriptGalleryScript, error) {
	client, token, err := a.galleryClientAndToken()
	if err != nil {
		return ScriptGalleryScript{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	script, err := client.UpdateScript(ctx, token, id, name, content)
	if err != nil {
		return ScriptGalleryScript{}, err
	}
	return mapGalleryScript(script), nil
}

func (a *App) DeleteScriptGalleryScript(id string) error {
	client, token, err := a.galleryClientAndToken()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return client.DeleteScript(ctx, token, id)
}

func (a *App) authenticateScriptGallery(password string, register bool) (ScriptGalleryAuthState, error) {
	identity, subject, status, err := a.currentGalleryIdentity()
	if err != nil {
		return ScriptGalleryAuthState{}, err
	}
	client, err := a.ensureGalleryClient()
	if err != nil {
		return ScriptGalleryAuthState{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var session gallerylib.AuthSession
	if register {
		session, err = client.Register(ctx, identity, password)
	} else {
		session, err = client.Login(ctx, identity, password)
	}
	if err != nil {
		return ScriptGalleryAuthState{}, err
	}
	if err := a.cacheGallerySession(status, subject, session); err != nil {
		return ScriptGalleryAuthState{}, err
	}
	return a.GetScriptGalleryAuthState()
}

func (a *App) ensureGalleryClient() (*gallerylib.Client, error) {
	if err := ensureAppRunning(a); err != nil {
		return nil, err
	}
	a.galleryMu.Lock()
	defer a.galleryMu.Unlock()
	if a.galleryClient == nil {
		client, err := gallerylib.NewClient()
		if err != nil {
			return nil, err
		}
		a.galleryClient = client
	}
	return a.galleryClient, nil
}

func (a *App) galleryClientAndToken() (*gallerylib.Client, string, error) {
	status := a.sessions.Status()
	_, subject, err := galleryIdentityFromStatus(status)
	if err != nil {
		return nil, "", err
	}
	client, token, err := a.galleryClientAndOptionalToken(status, subject)
	if err != nil {
		return nil, "", err
	}
	if token == "" {
		return nil, "", errors.New("sign in to the Script Gallery before changing projects or scripts")
	}
	return client, token, nil
}

func (a *App) galleryClientAndOptionalToken(status connection.Status, subject string) (*gallerylib.Client, string, error) {
	client, err := a.ensureGalleryClient()
	if err != nil {
		return nil, "", err
	}
	accountKey := galleryAccountKey(status)
	a.galleryMu.Lock()
	if a.galleryToken != "" && strings.EqualFold(a.gallerySubject, subject) && a.galleryTokenExpiresAt > time.Now().Add(time.Minute).Unix() {
		token := a.galleryToken
		a.galleryMu.Unlock()
		return client, token, nil
	}
	version := a.gallerySessionVersion
	store := a.gallerySessionStore
	a.galleryMu.Unlock()

	if store == nil || accountKey == "" {
		return client, "", nil
	}
	stored, ok, err := store.Load(accountKey)
	if err != nil {
		return client, "", nil
	}
	if !ok || !strings.EqualFold(stored.Subject, subject) || stored.ExpiresAt <= time.Now().Add(time.Minute).Unix() {
		return client, "", nil
	}
	a.galleryMu.Lock()
	if a.gallerySessionVersion != version {
		a.galleryMu.Unlock()
		return client, "", nil
	}
	a.galleryToken = stored.Token
	a.gallerySubject = stored.Subject
	a.galleryTokenExpiresAt = stored.ExpiresAt
	a.galleryUser = gallerylib.User{Username: stored.Username, DisplayName: stored.DisplayName}
	a.galleryMu.Unlock()
	return client, stored.Token, nil
}

func (a *App) cacheGallerySession(status connection.Status, subject string, session gallerylib.AuthSession) error {
	accountKey := galleryAccountKey(status)
	if accountKey == "" {
		return errors.New("the active RC account is unavailable")
	}
	if !strings.EqualFold(strings.TrimSpace(session.Subject), subject) {
		return errors.New("Script Gallery returned a session for a different account")
	}
	if strings.TrimSpace(session.Token) == "" || session.ExpiresAt <= time.Now().Unix() {
		return errors.New("Script Gallery returned an invalid session")
	}
	currentStatus := a.sessions.Status()
	_, currentSubject, err := galleryIdentityFromStatus(currentStatus)
	if err != nil || !strings.EqualFold(currentSubject, subject) || galleryAccountKey(currentStatus) != accountKey {
		return errors.New("the RC account changed while Script Gallery was signing in")
	}
	store := a.gallerySessionStore
	if store == nil {
		return errors.New("Script Gallery session storage is unavailable")
	}
	a.galleryMu.Lock()
	version := a.gallerySessionVersion
	a.galleryMu.Unlock()
	if err := store.Save(accountKey, gallerylib.StoredSession{
		Token:       session.Token,
		Subject:     session.Subject,
		Username:    session.User.Username,
		DisplayName: session.User.DisplayName,
		ExpiresAt:   session.ExpiresAt,
	}); err != nil {
		return err
	}
	a.galleryMu.Lock()
	defer a.galleryMu.Unlock()
	if a.gallerySessionVersion != version {
		return nil
	}
	a.galleryToken = session.Token
	a.gallerySubject = subject
	a.galleryTokenExpiresAt = session.ExpiresAt
	a.galleryUser = session.User
	return nil
}

func (a *App) clearGallerySession() {
	a.galleryMu.Lock()
	a.gallerySessionVersion++
	a.galleryToken = ""
	a.gallerySubject = ""
	a.galleryTokenExpiresAt = 0
	a.galleryUser = gallerylib.User{}
	a.galleryMu.Unlock()
}

func (a *App) currentGalleryIdentity() (gallerylib.Identity, string, connection.Status, error) {
	status := a.sessions.Status()
	identity, subject, err := galleryIdentityFromStatus(status)
	return identity, subject, status, err
}

func galleryIdentityFromStatus(status connection.Status) (gallerylib.Identity, string, error) {
	if !status.Connected || !status.Authenticated {
		return gallerylib.Identity{}, "", errors.New("connect to a server before opening the Script Gallery")
	}
	community := strings.TrimSpace(status.CommunityName)
	account := strings.TrimSpace(status.RealAccount)
	if account == "" {
		account = strings.TrimSpace(status.Account)
	}
	identity := gallerylib.Identity{CommunityName: community, AccountName: account}
	username := identity.Username()
	if username == "" {
		return gallerylib.Identity{}, "", errors.New("the server has not exposed a community or account identity yet")
	}
	return identity, strings.ToLower(username), nil
}

func galleryAccountKey(status connection.Status) string {
	account := strings.TrimSpace(status.Account)
	if account == "" {
		account = strings.TrimSpace(status.RealAccount)
	}
	return strings.ToLower(account)
}

func mapGalleryProject(project gallerylib.Project) ScriptGalleryProject {
	return ScriptGalleryProject{
		ID:            project.ID,
		Name:          project.Name,
		Description:   project.Description,
		Visibility:    project.Visibility,
		Owner:         project.Owner,
		Owned:         project.Owned,
		CreatedAt:     project.CreatedAt,
		UpdatedAt:     project.UpdatedAt,
		WeaponScripts: mapGalleryScripts(project.WeaponScripts),
		ClassScripts:  mapGalleryScripts(project.ClassScripts),
		NPCScripts:    mapGalleryScripts(project.NPCScripts),
	}
}

func mapGalleryScripts(scripts []gallerylib.Script) []ScriptGalleryScript {
	if scripts == nil {
		return nil
	}
	result := make([]ScriptGalleryScript, 0, len(scripts))
	for _, script := range scripts {
		result = append(result, mapGalleryScript(script))
	}
	return result
}

func mapGalleryScript(script gallerylib.Script) ScriptGalleryScript {
	return ScriptGalleryScript{
		ID:        script.ID,
		ProjectID: script.ProjectID,
		Type:      script.Type,
		Name:      script.Name,
		Content:   script.Content,
		CreatedAt: script.CreatedAt,
		UpdatedAt: script.UpdatedAt,
	}
}
