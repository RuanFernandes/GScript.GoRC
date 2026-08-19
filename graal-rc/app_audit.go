package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	auditlib "graal-rc/internal/audit"
	deploylib "graal-rc/internal/deploy"
)

type AuditEntry = auditlib.Entry
type DeploymentBackup = deploylib.Backup

func newLocalChangeStores() (*auditlib.Store, *deploylib.Store, *changeRetentionStore) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("change history: %v", err)
		return nil, nil, nil
	}
	root := filepath.Join(configDir, "graal-rc")
	var auditStore *auditlib.Store
	if store, err := auditlib.New(filepath.Join(root, "audit.jsonl")); err != nil {
		log.Printf("audit store: %v", err)
	} else {
		auditStore = store
	}
	var backupStore *deploylib.Store
	if store, err := deploylib.New(filepath.Join(root, "backups")); err != nil {
		log.Printf("backup store: %v", err)
	} else {
		backupStore = store
	}
	var retentionStore *changeRetentionStore
	if store, err := newChangeRetentionStore(filepath.Join(root, "retention.json")); err != nil {
		log.Printf("retention store: %v", err)
	} else {
		retentionStore = store
	}
	return auditStore, backupStore, retentionStore
}

func (a *App) recordAudit(action, resource, target, outcome, detail string) {
	if a == nil || a.audit == nil {
		return
	}
	status := a.sessions.Status()
	if err := a.audit.Record(AuditEntry{
		Server:   status.ServerName,
		Account:  status.Account,
		Action:   action,
		Resource: resource,
		Target:   target,
		Outcome:  outcome,
		Detail:   detail,
	}); err != nil {
		log.Printf("audit %s %s: %v", action, target, err)
		return
	}
	a.emitChangeHistoryChanged()
}

func (a *App) saveDeploymentBackup(resource, target string, content []byte, hasPrevious bool) (DeploymentBackup, bool, error) {
	if !hasPrevious || a == nil || a.backups == nil {
		return DeploymentBackup{}, false, nil
	}
	a.backupPolicyMu.Lock()
	defer a.backupPolicyMu.Unlock()
	status := a.sessions.Status()
	meta := DeploymentBackup{
		Server:   status.ServerName,
		Account:  status.Account,
		Resource: resource,
		Target:   target,
	}
	backupLimit := defaultBackupCount
	if a.retention != nil {
		settings, err := a.retention.Load()
		if err != nil {
			log.Printf("backup count: %v; using default %d", err, defaultBackupCount)
		} else {
			backupLimit = settings.BackupCount
		}
	}
	backup, err := a.backups.SaveLimited(meta, content, backupLimit)
	if err != nil {
		return DeploymentBackup{}, false, err
	}
	return backup, true, nil
}

// backupSyncScript is called before Local Sync overwrites an existing local
// script with the server version. A missing backup store remains best-effort,
// matching the behavior of the existing deployment history helpers.
func (a *App) backupSyncScript(kind, key string, previous []byte) (string, error) {
	backup, ok, err := a.saveDeploymentBackup("script", kind+":"+key, previous, true)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return backup.ID, nil
}

func (a *App) auditSyncPull(kind, key, outcome, detail string) {
	a.recordAudit("sync", "script", kind+":"+key, outcome, detail)
	if a != nil && a.audit == nil {
		a.emitChangeHistoryChanged()
	}
}

func (a *App) editorOriginal(kind, key string) ([]byte, bool) {
	if a == nil {
		return nil, false
	}
	a.editorCacheMu.Lock()
	reply, ok := a.editorCache[kind+":"+key]
	a.editorCacheMu.Unlock()
	if !ok {
		return nil, false
	}
	return append([]byte(nil), []byte(reply.Script)...), true
}

func (a *App) editorOriginalForKind(kind string) ([]byte, string, bool) {
	if a == nil {
		return nil, "", false
	}
	prefix := kind + ":"
	a.editorCacheMu.Lock()
	defer a.editorCacheMu.Unlock()
	for mapKey, reply := range a.editorCache {
		if !strings.HasPrefix(mapKey, prefix) {
			continue
		}
		return append([]byte(nil), []byte(reply.Script)...), strings.TrimPrefix(mapKey, prefix), true
	}
	return nil, "", false
}

func (a *App) updateEditorContent(kind, key, content string) {
	if a == nil {
		return
	}
	a.editorCacheMu.Lock()
	if reply, ok := a.editorCache[kind+":"+key]; ok {
		reply.Script = content
		a.editorCache[kind+":"+key] = reply
	}
	a.editorCacheMu.Unlock()
}

func (a *App) backupRemoteContent(resource, target string) (DeploymentBackup, bool, error) {
	content, err := a.sessions.DownloadFile(target)
	if err != nil {
		// A missing remote file is a valid first upload/delete case. The remote
		// mutation still gets audited with a missing-backup detail.
		return DeploymentBackup{}, false, nil
	}
	return a.saveDeploymentBackup(resource, target, content, true)
}

func (a *App) loadRemoteTextForBackup(target string) ([]byte, bool, error) {
	content, err := a.sessions.DownloadFile(target)
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func (a *App) GetAuditEntries(limit int) ([]AuditEntry, error) {
	if a == nil || a.audit == nil {
		return []AuditEntry{}, nil
	}
	if a.retention != nil {
		if settings, err := a.retention.Load(); err == nil {
			if cleanupErr := a.cleanupExpiredChangeData(settings); cleanupErr != nil {
				log.Printf("change retention cleanup: %v", cleanupErr)
			}
		} else {
			log.Printf("change retention: %v", err)
		}
	}
	return a.audit.List(limit)
}

func (a *App) ClearAuditEntries() error {
	if a == nil || a.audit == nil {
		return nil
	}
	if err := a.audit.Clear(); err != nil {
		return err
	}
	a.emitChangeHistoryChanged()
	return nil
}

func (a *App) GetDeploymentBackups(limit int) ([]DeploymentBackup, error) {
	if a == nil || a.backups == nil {
		return []DeploymentBackup{}, nil
	}
	if a.retention != nil {
		if settings, err := a.retention.Load(); err == nil {
			if cleanupErr := a.cleanupExpiredChangeData(settings); cleanupErr != nil {
				log.Printf("change retention cleanup: %v", cleanupErr)
			}
		} else {
			log.Printf("change retention: %v", err)
		}
	}
	return a.backups.List(limit)
}

const maxDeploymentDiffTextBytes = 8 * 1024 * 1024

func (a *App) validateDeploymentBackupServer(meta deploylib.Backup) error {
	status := a.sessions.Status()
	if meta.Server != "" && meta.Server != status.ServerName {
		return fmt.Errorf("backup belongs to server %q, but the current server is %q", meta.Server, status.ServerName)
	}
	return nil
}

func (a *App) readCurrentDeploymentContent(meta deploylib.Backup) ([]byte, error) {
	switch meta.Resource {
	case "script":
		kind, key, ok := strings.Cut(meta.Target, ":")
		if !ok || kind == "" || key == "" {
			return nil, errors.New("invalid script backup target")
		}
		switch kind {
		case "weapon", "class", "npc":
		default:
			return nil, fmt.Errorf("unsupported script type %q", kind)
		}
		reply, err := a.sessions.OpenScript(kind, key)
		if err != nil {
			return nil, err
		}
		return []byte(reply.Script), nil
	case "npcflags":
		id, err := strconv.Atoi(meta.Target)
		if err != nil {
			return nil, err
		}
		reply, err := a.sessions.OpenNPCFlags(id)
		if err != nil {
			return nil, err
		}
		return []byte(reply.Script), nil
	case "servertext":
		reply, err := a.sessions.OpenServerText(meta.Target)
		if err != nil {
			return nil, err
		}
		return []byte(reply.Script), nil
	case "textfile", "file", "sqlite":
		return a.sessions.DownloadFile(meta.Target)
	default:
		return nil, fmt.Errorf("unsupported backup resource %q", meta.Resource)
	}
}

func deploymentBackupLanguage(meta deploylib.Backup) string {
	switch meta.Resource {
	case "script":
		return "graalscript"
	case "npcflags", "servertext":
		return "serverconfig"
	}

	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(meta.Target), ".")) {
	case "json":
		return "json"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "html":
		return "html"
	case "css":
		return "css"
	case "xml":
		return "xml"
	case "md":
		return "markdown"
	case "yaml", "yml":
		return "yaml"
	default:
		return "plaintext"
	}
}

func newDeploymentBackupDiff(meta deploylib.Backup, backupContent, currentContent []byte) deploylib.BackupDiff {
	diff := deploylib.BackupDiff{
		Backup:        meta,
		Language:      deploymentBackupLanguage(meta),
		CurrentExists: true,
		CurrentSize:   int64(len(currentContent)),
	}
	currentDigest := sha256.Sum256(currentContent)
	diff.CurrentSHA256 = hex.EncodeToString(currentDigest[:])

	if len(backupContent) > maxDeploymentDiffTextBytes || len(currentContent) > maxDeploymentDiffTextBytes {
		diff.DiffReason = "too_large"
		return diff
	}
	if !utf8.Valid(backupContent) || !utf8.Valid(currentContent) {
		diff.DiffReason = "binary"
		return diff
	}

	diff.BackupContent = string(backupContent)
	diff.CurrentContent = string(currentContent)
	diff.Diffable = true
	return diff
}

// GetDeploymentBackupDiff reads a saved snapshot and fetches the current
// remote version without changing either one. Restore remains a separate
// operation so the review screen can be opened safely before confirmation.
func (a *App) GetDeploymentBackupDiff(backupID string) (deploylib.BackupDiff, error) {
	if a == nil || a.backups == nil {
		return deploylib.BackupDiff{}, errors.New("backup store is unavailable")
	}
	meta, backupContent, err := a.backups.Read(backupID)
	if err != nil {
		return deploylib.BackupDiff{}, err
	}
	if err := a.validateDeploymentBackupServer(meta); err != nil {
		return deploylib.BackupDiff{}, err
	}
	currentContent, err := a.readCurrentDeploymentContent(meta)
	if err != nil {
		return deploylib.BackupDiff{}, fmt.Errorf("read current server version: %w", err)
	}
	return newDeploymentBackupDiff(meta, backupContent, currentContent), nil
}

func (a *App) DeleteDeploymentBackup(backupID string) error {
	if a == nil || a.backups == nil {
		return errors.New("backup store is unavailable")
	}
	meta, err := a.backups.Delete(backupID)
	if err != nil {
		return err
	}
	a.recordAudit("delete", "backup", meta.Target, "success", "deleted backup "+meta.ID)
	if a.audit == nil {
		a.emitChangeHistoryChanged()
	}
	return nil
}

func (a *App) OpenDeploymentCenter() {
	if a == nil || a.app == nil {
		return
	}
	a.deploymentMu.Lock()
	defer a.deploymentMu.Unlock()
	if a.deploymentWindow != nil {
		a.deploymentWindow.Show()
		a.deploymentWindow.Focus()
		return
	}
	server := a.sessions.Status().ServerName
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "deployment-center",
		Title:            serverWindowTitle(server, "Change history"),
		URL:              "/#deployments",
		Width:            1040,
		Height:           720,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.deploymentWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.deploymentMu.Lock()
		if a.deploymentWindow == w {
			a.deploymentWindow = nil
		}
		a.deploymentMu.Unlock()
	})
}

func (a *App) RollbackDeployment(backupID string) error {
	if a == nil || a.backups == nil {
		return errors.New("backup store is unavailable")
	}
	meta, content, err := a.backups.Read(backupID)
	if err != nil {
		return err
	}
	if err := a.validateDeploymentBackupServer(meta); err != nil {
		return err
	}

	var rollbackErr error
	switch meta.Resource {
	case "script":
		kind, key, ok := strings.Cut(meta.Target, ":")
		if !ok || kind == "" || key == "" {
			rollbackErr = errors.New("invalid script backup target")
			break
		}
		rollbackErr = a.saveScriptWithSyncExpectation(kind, key, string(content), func() error {
			switch kind {
			case "weapon":
				return a.sessions.SaveWeapon(key, string(content))
			case "class":
				return a.sessions.SaveClass(key, string(content))
			case "npc":
				id, parseErr := strconv.Atoi(key)
				if parseErr != nil {
					return parseErr
				}
				return a.sessions.SaveNPC(id, string(content))
			default:
				return fmt.Errorf("unsupported script type %q", kind)
			}
		})
	case "npcflags":
		id, parseErr := strconv.Atoi(meta.Target)
		if parseErr != nil {
			rollbackErr = parseErr
			break
		}
		rollbackErr = a.sessions.SaveNPCFlags(id, string(content))
	case "servertext":
		rollbackErr = a.sessions.UploadServerText(meta.Target, string(content))
	case "textfile", "file", "sqlite":
		rollbackErr = a.sessions.UploadFile(meta.Target, content)
	default:
		rollbackErr = fmt.Errorf("unsupported backup resource %q", meta.Resource)
	}
	if rollbackErr != nil {
		a.recordAudit("rollback", meta.Resource, meta.Target, "failed", rollbackErr.Error())
		return rollbackErr
	}
	a.recordAudit("rollback", meta.Resource, meta.Target, "success", "restored backup "+meta.ID)
	return nil
}
