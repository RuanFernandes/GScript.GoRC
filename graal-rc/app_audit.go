package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	status := a.sessions.Status()
	backup, err := a.backups.Save(DeploymentBackup{
		Server:   status.ServerName,
		Account:  status.Account,
		Resource: resource,
		Target:   target,
	}, content)
	if err != nil {
		return DeploymentBackup{}, false, err
	}
	return backup, true, nil
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
	title := "Change history"
	if server != "" {
		title += " · " + server
	}
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "deployment-center",
		Title:            title,
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
		a.deploymentWindow = nil
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
	status := a.sessions.Status()
	if meta.Server != "" && meta.Server != status.ServerName {
		return fmt.Errorf("backup belongs to server %q, but the current server is %q", meta.Server, status.ServerName)
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
