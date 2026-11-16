package snapshots

import (
	"cloudforge/core/internal/databases"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"time"
)

type Snapshot struct {
	ID           string    `json:"id"`
	DriveID      string    `json:"driveId"`
	DatabaseID   string    `json:"databaseId"`
	Engine       string    `json:"engine"`
	Created      time.Time `json:"created"`
	Mode         string    `json:"mode"`
	Size         int64     `json:"size"`
	Name         string    `json:"name"`
	Format       string    `json:"format"`
	Version      string    `json:"version"`
	Folder       string    `json:"folder"`
	Installation string    `json:"installation"`
	Machine      string    `json:"machine"`
	Checksum     string    `json:"checksum"`
}

func taggedFolder(item driveFile, kind string) bool {
	return item.MIME == folderMIME && driveID.MatchString(item.ID) && item.Props["cfKind"] == kind &&
		item.Props["installation"] != ""
}

func matchesParent(item driveFile, parent string) bool {
	return len(item.Parents) == 1 && item.Parents[0] == parent
}

func matchesDatabase(item driveFile, installation, database, engine string) bool {
	return taggedFolder(item, "database") && item.Props["installation"] == installation &&
		item.Props["database"] == database && item.Props["engine"] == engine
}

func (s *Service) cachedFolder(ctx context.Context, accountID, installation, id string, valid func(driveFile) bool, query string, create func() (driveFile, error)) (driveFile, error) {
	var folderID string
	err := s.store.DB.QueryRowContext(ctx, "SELECT drive_id FROM snapshot_drive_folders WHERE account_id=? AND installation_id=? AND database_id=?", accountID, installation, id).Scan(&folderID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return driveFile{}, err
	}
	if folderID != "" {
		item, found, err := s.drive.get(ctx, accountID, folderID)
		if err != nil {
			return driveFile{}, err
		}
		if found && valid(item) {
			return item, nil
		}
	}
	items, err := s.drive.list(ctx, accountID, "mimeType = "+qvalue(folderMIME)+" and "+query)
	if err != nil {
		return driveFile{}, err
	}
	sortDriveFiles(items)
	var selected driveFile
	for _, item := range items {
		if valid(item) {
			selected = item
		}
	}
	if selected.ID == "" && create != nil {
		selected, err = create()
		if err != nil {
			return driveFile{}, err
		}
		if !valid(selected) {
			return driveFile{}, errors.New("o Google Drive não confirmou a criação da pasta de snapshots")
		}
	}
	if selected.ID != "" {
		_, err = s.store.DB.ExecContext(ctx, "INSERT OR REPLACE INTO snapshot_drive_folders VALUES(?,?,?,?)", accountID, installation, id, selected.ID)
	}
	return selected, err
}

func (s *Service) folder(ctx context.Context, accountID string, db databases.Instance, create bool) (driveFile, error) {
	s.folders.Lock()
	defer s.folders.Unlock()
	install := s.installation
	inInstallation := property("installation", install)
	var createMachine func() (driveFile, error)
	if create {
		createMachine = func() (driveFile, error) {
			return s.drive.createFolder(ctx, accountID, "cloudforge - "+computerModel(ctx, s.store), "", map[string]string{"cfKind": "machine", "installation": install})
		}
	}
	root, err := s.cachedFolder(ctx, accountID, install, "", func(f driveFile) bool {
		return taggedFolder(f, "machine") && f.Props["installation"] == install
	}, property("cfKind", "machine")+" and "+inInstallation, createMachine)
	if err != nil || root.ID == "" {
		return driveFile{}, err
	}
	var createDatabase func() (driveFile, error)
	if create {
		createDatabase = func() (driveFile, error) {
			return s.drive.createFolder(ctx, accountID, databases.SnapshotEngineName(db.Engine)+" - "+db.ID, root.ID, map[string]string{
				"cfKind": "database", "installation": install, "database": db.ID, "engine": db.Engine,
			})
		}
	}
	return s.cachedFolder(ctx, accountID, install, db.ID, func(f driveFile) bool {
		return matchesParent(f, root.ID) && matchesDatabase(f, install, db.ID, db.Engine)
	}, qvalue(root.ID)+" in parents and "+property("cfKind", "database")+" and "+property("database", db.ID)+" and "+inInstallation, createDatabase)
}

func fileSnapshot(item, folder driveFile, machine string) (Snapshot, bool) {
	p := item.Props
	engine := folder.Props["engine"]
	if !driveID.MatchString(item.ID) || !driveID.MatchString(p["snapshot"]) || item.Created.IsZero() ||
		!matchesParent(item, folder.ID) || p["cfKind"] != "snapshot" ||
		p["installation"] != folder.Props["installation"] || p["database"] != folder.Props["database"] ||
		p["engine"] != engine || p["format"] != databases.SnapshotFormat(engine) || p["version"] == "" ||
		(p["mode"] != "manual" && p["mode"] != "automatic") || !snapshotName.MatchString(item.Name) ||
		!strings.HasSuffix(item.Name, "."+p["format"]) || !item.verified() {
		return Snapshot{}, false
	}
	return Snapshot{ID: p["snapshot"], DriveID: item.ID, DatabaseID: p["database"], Engine: engine, Created: item.Created,
		Mode: p["mode"], Size: item.Size, Name: item.Name, Format: p["format"], Version: p["version"], Folder: folder.ID,
		Installation: p["installation"], Machine: machine, Checksum: p["sha256"]}, true
}

func sortDriveFiles(items []driveFile) {
	slices.SortFunc(items, func(a, b driveFile) int {
		if cmp := b.Created.Compare(a.Created); cmp != 0 {
			return cmp
		}
		return strings.Compare(b.ID, a.ID)
	})
}

func (s *Service) listFolder(ctx context.Context, accountID string, folder driveFile) ([]driveFile, error) {
	items, err := s.drive.list(ctx, accountID, qvalue(folder.ID)+" in parents and "+property("cfKind", "snapshot"))
	if err != nil {
		return nil, err
	}
	items = slices.DeleteFunc(items, func(f driveFile) bool { _, valid := fileSnapshot(f, folder, ""); return !valid })
	sortDriveFiles(items)
	return items, nil
}

func (s *Service) reconcile(ctx context.Context, accountID string, folder driveFile, items []driveFile) ([]Snapshot, error) {
	list := make([]Snapshot, 0, len(items))
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM snapshot_history WHERE account_id=? AND origin_folder=?", accountID, folder.ID); err != nil {
		return nil, err
	}
	for _, item := range items {
		row, valid := fileSnapshot(item, folder, "")
		if !valid {
			continue
		}
		payload, _ := json.Marshal(row)
		if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO snapshot_history VALUES(?,?,?,?)", accountID, folder.ID, item.ID, payload); err != nil {
			return nil, err
		}
		list = append(list, row)
	}
	return list, tx.Commit()
}

func (s *Service) retain(ctx context.Context, accountID string, folder driveFile, limit int, verified ...driveFile) error {
	items, err := s.listFolder(ctx, accountID, folder)
	if err != nil {
		return err
	}
	for _, item := range verified {
		if !slices.ContainsFunc(items, func(f driveFile) bool { return f.ID == item.ID }) {
			items = append(items, item)
		}
	}
	sortDriveFiles(items)
	limit = min(max(1, limit), 50)
	if len(items) > limit {
		for _, old := range items[limit:] {
			// Recheck parent and provenance after listing, before permanently deleting.
			item, exists, err := s.drive.get(ctx, accountID, old.ID)
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
			if _, valid := fileSnapshot(item, folder, ""); !valid {
				continue
			}
			if err = s.drive.delete(ctx, accountID, item.ID); err != nil {
				return err
			}
		}
		items = items[:limit]
	}
	_, err = s.reconcile(ctx, accountID, folder, items)
	return err
}

func (s *Service) remoteItem(ctx context.Context, accountID, id, engine string) (driveFile, error) {
	item, found, err := s.drive.get(ctx, accountID, id)
	if err != nil {
		return item, err
	}
	if !found || len(item.Parents) != 1 || item.Props["engine"] != engine {
		return driveFile{}, errors.New("snapshot indisponível ou incompatível com o banco de destino")
	}
	folder, found, err := s.drive.get(ctx, accountID, item.Parents[0])
	if err != nil {
		return item, err
	}
	if !found || !taggedFolder(folder, "database") || len(folder.Parents) != 1 {
		return driveFile{}, errors.New("a pasta de origem do snapshot é inválida")
	}
	root, found, err := s.drive.get(ctx, accountID, folder.Parents[0])
	if err != nil {
		return item, err
	}
	_, valid := fileSnapshot(item, folder, "")
	if !found || !taggedFolder(root, "machine") || root.Props["installation"] != folder.Props["installation"] || !valid {
		return driveFile{}, errors.New("não foi possível verificar o snapshot no Google Drive")
	}
	return item, nil
}

func (s *Service) Recoverable(ctx context.Context, owner, id string) ([]Snapshot, error) {
	db, err := s.databases.SnapshotDatabase(owner, id)
	if err != nil {
		return nil, err
	}
	s.accountGate.RLock()
	defer s.accountGate.RUnlock()
	accountID, err := s.connected()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	folders, err := s.drive.list(ctx, accountID, "mimeType = "+qvalue(folderMIME)+" and "+property("cfKind", "database")+" and "+property("engine", db.Engine))
	if err != nil {
		return nil, err
	}
	machines, err := s.drive.list(ctx, accountID, "mimeType = "+qvalue(folderMIME)+" and "+property("cfKind", "machine"))
	if err != nil {
		return nil, err
	}
	roots := make(map[string]driveFile, len(machines))
	for _, root := range machines {
		if taggedFolder(root, "machine") {
			roots[root.ID] = root
		}
	}
	result := []Snapshot{}
	for _, folder := range folders {
		if len(folder.Parents) != 1 || !taggedFolder(folder, "database") || folder.Props["engine"] != db.Engine {
			continue
		}
		root, ok := roots[folder.Parents[0]]
		if !ok || root.Props["installation"] != folder.Props["installation"] ||
			(folder.Props["installation"] == s.installation && folder.Props["database"] == id) {
			continue
		}
		items, err := s.listFolder(ctx, accountID, folder)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if row, valid := fileSnapshot(item, folder, root.Name); valid {
				result = append(result, row)
			}
		}
	}
	slices.SortFunc(result, func(a, b Snapshot) int { return b.Created.Compare(a.Created) })
	return result, nil
}

func (s *Service) stageFile() (*os.File, error) {
	return os.CreateTemp(s.workDir, "cloudforge-*.part")
}
