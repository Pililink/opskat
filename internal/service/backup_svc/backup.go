package backup_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/opskat/opskat/internal/model/entity/asset_entity"
	"github.com/opskat/opskat/internal/model/entity/credential_entity"
	"github.com/opskat/opskat/internal/model/entity/forward_entity"
	"github.com/opskat/opskat/internal/model/entity/group_entity"
	"github.com/opskat/opskat/internal/model/entity/policy_group_entity"
	"github.com/opskat/opskat/internal/repository/asset_repo"
	"github.com/opskat/opskat/internal/repository/credential_repo"
	"github.com/opskat/opskat/internal/repository/forward_repo"
	"github.com/opskat/opskat/internal/repository/group_repo"
	"github.com/opskat/opskat/internal/repository/policy_group_repo"
)

// CredentialCrypto 凭证加解密接口
type CredentialCrypto interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// BackupCredential 凭据备份条目（含明文敏感字段）
type BackupCredential struct {
	credential_entity.Credential
	PlainPassword   string `json:"plain_password,omitempty"`
	PlainPrivateKey string `json:"plain_private_key,omitempty"`
}

// BackupForward 端口转发备份条目（config + rules 打包）
type BackupForward struct {
	forward_entity.ForwardConfig
	Rules []*forward_entity.ForwardRule `json:"rules"`
}

// BackupData 备份数据结构
type BackupData struct {
	Version             string                             `json:"version"`
	ExportedAt          string                             `json:"exported_at"`
	IncludesCredentials bool                               `json:"includes_credentials,omitempty"`
	Groups              []*group_entity.Group              `json:"groups,omitempty"`
	Assets              []*asset_entity.Asset              `json:"assets,omitempty"`
	Credentials         []*BackupCredential                `json:"credentials,omitempty"`
	PolicyGroups        []*policy_group_entity.PolicyGroup `json:"policy_groups,omitempty"`
	Forwards            []*BackupForward                   `json:"forwards,omitempty"`
	Shortcuts           json.RawMessage                    `json:"shortcuts,omitempty"`
	CustomThemes        json.RawMessage                    `json:"custom_themes,omitempty"`
}

// BackupSummary 备份概览信息（用于导入前预览）
type BackupSummary struct {
	Version             string `json:"version"`
	ExportedAt          string `json:"exported_at"`
	Encrypted           bool   `json:"encrypted"`
	IncludesCredentials bool   `json:"includes_credentials"`
	AssetCount          int    `json:"asset_count"`
	GroupCount          int    `json:"group_count"`
	CredentialCount     int    `json:"credential_count"`
	PolicyGroupCount    int    `json:"policy_group_count"`
	ForwardCount        int    `json:"forward_count"`
	HasShortcuts        bool   `json:"has_shortcuts"`
	HasCustomThemes     bool   `json:"has_custom_themes"`
}

// Summary 返回备份概览
func (d *BackupData) Summary() *BackupSummary {
	return &BackupSummary{
		Version:             d.Version,
		ExportedAt:          d.ExportedAt,
		IncludesCredentials: d.IncludesCredentials,
		AssetCount:          len(d.Assets),
		GroupCount:          len(d.Groups),
		CredentialCount:     len(d.Credentials),
		PolicyGroupCount:    len(d.PolicyGroups),
		ForwardCount:        len(d.Forwards),
		HasShortcuts:        len(d.Shortcuts) > 0,
		HasCustomThemes:     len(d.CustomThemes) > 0,
	}
}

// ExportOptions 导出选项
type ExportOptions struct {
	AssetIDs            []int64 `json:"asset_ids"`             // 空=全部
	IncludeCredentials  bool    `json:"include_credentials"`   // 包含凭据（强制加密）
	IncludeForwards     bool    `json:"include_forwards"`      // 包含端口转发
	IncludePolicyGroups bool    `json:"include_policy_groups"` // 包含策略组
	Shortcuts           string  `json:"shortcuts,omitempty"`   // JSON 字符串
	CustomThemes        string  `json:"custom_themes,omitempty"`
}

// ImportOptions 导入选项
type ImportOptions struct {
	ImportAssets       bool   `json:"import_assets"`
	ImportCredentials  bool   `json:"import_credentials"`
	ImportForwards     bool   `json:"import_forwards"`
	ImportPolicyGroups bool   `json:"import_policy_groups"`
	ImportShortcuts    bool   `json:"import_shortcuts"`
	ImportThemes       bool   `json:"import_themes"`
	Mode               string `json:"mode"` // "replace" | "merge"
}

// ImportResult 导入结果
type ImportResult struct {
	AssetsImported       int    `json:"assets_imported"`
	GroupsImported       int    `json:"groups_imported"`
	CredentialsImported  int    `json:"credentials_imported"`
	PolicyGroupsImported int    `json:"policy_groups_imported"`
	ForwardsImported     int    `json:"forwards_imported"`
	Shortcuts            string `json:"shortcuts,omitempty"` // JSON 字符串，前端处理
	CustomThemes         string `json:"custom_themes,omitempty"`
}

// Export 导出数据
func Export(ctx context.Context, opts *ExportOptions, crypto CredentialCrypto) (*BackupData, error) {
	allAssets, err := asset_repo.Asset().List(ctx, asset_repo.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("导出资产失败: %w", err)
	}
	allGroups, err := group_repo.Group().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("导出分组失败: %w", err)
	}

	// 构建查找映射
	assetMap := make(map[int64]*asset_entity.Asset, len(allAssets))
	for _, a := range allAssets {
		assetMap[a.ID] = a
	}
	groupMap := make(map[int64]*group_entity.Group, len(allGroups))
	for _, g := range allGroups {
		groupMap[g.ID] = g
	}

	// 确定要导出的资产
	var selectedAssets []*asset_entity.Asset
	if len(opts.AssetIDs) > 0 {
		selectedIDs := make(map[int64]bool)
		for _, id := range opts.AssetIDs {
			selectedIDs[id] = true
		}
		// 自动解析依赖
		resolveDependentAssets(selectedIDs, assetMap)
		for _, a := range allAssets {
			if selectedIDs[a.ID] {
				selectedAssets = append(selectedAssets, a)
			}
		}
	} else {
		selectedAssets = allAssets
	}

	// 收集所需的分组（含祖先链）
	neededGroupIDs := make(map[int64]bool)
	for _, a := range selectedAssets {
		if a.GroupID > 0 {
			collectAncestorGroups(a.GroupID, groupMap, neededGroupIDs)
		}
	}
	var selectedGroups []*group_entity.Group
	for _, g := range allGroups {
		if neededGroupIDs[g.ID] {
			selectedGroups = append(selectedGroups, g)
		}
	}
	// 如果导出全部资产，导出全部分组
	if len(opts.AssetIDs) == 0 {
		selectedGroups = allGroups
	}

	data := &BackupData{
		Version:    "1.0",
		ExportedAt: time.Now().Format(time.RFC3339),
		Assets:     selectedAssets,
		Groups:     selectedGroups,
	}
	if opts.Shortcuts != "" {
		data.Shortcuts = json.RawMessage(opts.Shortcuts)
	}
	if opts.CustomThemes != "" {
		data.CustomThemes = json.RawMessage(opts.CustomThemes)
	}

	// 收集选中资产 ID 集合（后续模块使用）
	selectedAssetIDs := make(map[int64]bool, len(selectedAssets))
	for _, a := range selectedAssets {
		selectedAssetIDs[a.ID] = true
	}

	// 收集策略组
	if opts.IncludePolicyGroups {
		pgIDs := collectPolicyGroupIDs(selectedAssets, selectedGroups)
		if len(pgIDs) > 0 {
			ids := make([]int64, 0, len(pgIDs))
			for id := range pgIDs {
				ids = append(ids, id)
			}
			pgs, err := policy_group_repo.PolicyGroup().ListByIDs(ctx, ids)
			if err != nil {
				return nil, fmt.Errorf("导出策略组失败: %w", err)
			}
			data.PolicyGroups = pgs
		}
	}

	// 凭据处理
	if opts.IncludeCredentials && crypto != nil {
		data.IncludesCredentials = true
		creds, err := exportCredentials(ctx, selectedAssets, crypto)
		if err != nil {
			return nil, fmt.Errorf("导出凭据失败: %w", err)
		}
		data.Credentials = creds
		// 解密资产内联密码
		if err := decryptAssetPasswords(data.Assets, crypto); err != nil {
			return nil, fmt.Errorf("解密资产密码失败: %w", err)
		}
	} else {
		// 不含凭据：清除敏感字段
		stripAssetSecrets(data.Assets)
	}

	// 端口转发
	if opts.IncludeForwards {
		forwards, err := exportForwards(ctx, selectedAssetIDs)
		if err != nil {
			return nil, fmt.Errorf("导出端口转发失败: %w", err)
		}
		data.Forwards = forwards
	}

	return data, nil
}

// Import 导入备份数据
func Import(ctx context.Context, data *BackupData, opts *ImportOptions, crypto CredentialCrypto) (*ImportResult, error) {
	result := &ImportResult{}
	isReplace := opts.Mode != "merge"

	err := db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 策略组
		pgIDMap := make(map[int64]int64)
		if opts.ImportPolicyGroups && len(data.PolicyGroups) > 0 {
			if isReplace {
				if err := tx.Exec("DELETE FROM policy_groups").Error; err != nil {
					return fmt.Errorf("清除策略组失败: %w", err)
				}
			}
			for _, pg := range data.PolicyGroups {
				oldID := pg.ID
				pg.ID = 0
				if err := tx.Create(pg).Error; err != nil {
					return fmt.Errorf("创建策略组 %s 失败: %w", pg.Name, err)
				}
				pgIDMap[oldID] = pg.ID
				result.PolicyGroupsImported++
			}
		}

		// 2. 凭据
		credIDMap := make(map[int64]int64)
		if opts.ImportCredentials && len(data.Credentials) > 0 && crypto != nil {
			if isReplace {
				if err := tx.Exec("DELETE FROM credentials").Error; err != nil {
					return fmt.Errorf("清除凭据失败: %w", err)
				}
			}
			for _, bc := range data.Credentials {
				oldID := bc.ID
				cred := bc.Credential
				cred.ID = 0
				// 重新加密
				if bc.PlainPassword != "" {
					encrypted, err := crypto.Encrypt(bc.PlainPassword)
					if err != nil {
						return fmt.Errorf("加密密码失败: %w", err)
					}
					cred.Password = encrypted
				}
				if bc.PlainPrivateKey != "" {
					encrypted, err := crypto.Encrypt(bc.PlainPrivateKey)
					if err != nil {
						return fmt.Errorf("加密私钥失败: %w", err)
					}
					cred.PrivateKey = encrypted
				}
				if err := tx.Create(&cred).Error; err != nil {
					return fmt.Errorf("创建凭据 %s 失败: %w", cred.Name, err)
				}
				credIDMap[oldID] = cred.ID
				result.CredentialsImported++
			}
		}

		// 3. 分组
		groupIDMap := make(map[int64]int64)
		if opts.ImportAssets && len(data.Groups) > 0 {
			if isReplace {
				if err := tx.Exec("DELETE FROM groups").Error; err != nil {
					return fmt.Errorf("清除分组失败: %w", err)
				}
			}
			sortedGroups := sortGroups(data.Groups)
			for _, g := range sortedGroups {
				oldID := g.ID
				g.ID = 0
				if g.ParentID > 0 {
					if newID, ok := groupIDMap[g.ParentID]; ok {
						g.ParentID = newID
					}
				}
				// 回填策略组引用
				remapGroupPolicyGroupIDs(g, pgIDMap)
				if err := tx.Create(g).Error; err != nil {
					return fmt.Errorf("创建分组 %s 失败: %w", g.Name, err)
				}
				groupIDMap[oldID] = g.ID
				result.GroupsImported++
			}
		}

		// 4. 资产
		assetIDMap := make(map[int64]int64)
		if opts.ImportAssets && len(data.Assets) > 0 {
			if isReplace {
				if err := tx.Exec("DELETE FROM assets").Error; err != nil {
					return fmt.Errorf("清除资产失败: %w", err)
				}
			}

			type deferredRef struct {
				newAssetID int64
				oldRefID   int64
				refType    string // "jump_host" | "ssh_tunnel"
			}
			var deferredRefs []deferredRef

			for _, a := range data.Assets {
				oldID := a.ID
				a.ID = 0
				if a.GroupID > 0 {
					if newID, ok := groupIDMap[a.GroupID]; ok {
						a.GroupID = newID
					}
				}
				// 回填策略组引用
				remapAssetPolicyGroupIDs(a, pgIDMap)
				// 处理 Config 中的引用
				var oldJumpHostID, oldSSHAssetID int64
				switch {
				case a.IsSSH() && a.Config != "":
					cfg, err := a.GetSSHConfig()
					if err == nil {
						if cfg.JumpHostID > 0 {
							oldJumpHostID = cfg.JumpHostID
							cfg.JumpHostID = 0
						}
						// 回填 CredentialID
						if cfg.CredentialID > 0 {
							if newID, ok := credIDMap[cfg.CredentialID]; ok {
								cfg.CredentialID = newID
							} else if !opts.ImportCredentials {
								cfg.CredentialID = 0
							}
						}
						// 重新加密内联密码
						if data.IncludesCredentials && cfg.Password != "" && crypto != nil {
							encrypted, encErr := crypto.Encrypt(cfg.Password)
							if encErr != nil {
								logger.Default().Warn("re-encrypt ssh password", zap.Error(encErr))
							} else {
								cfg.Password = encrypted
							}
						}
						// 代理密码
						if data.IncludesCredentials && cfg.Proxy != nil && cfg.Proxy.Password != "" && crypto != nil {
							encrypted, encErr := crypto.Encrypt(cfg.Proxy.Password)
							if encErr != nil {
								logger.Default().Warn("re-encrypt proxy password", zap.Error(encErr))
							} else {
								cfg.Proxy.Password = encrypted
							}
						}
						if err := a.SetSSHConfig(cfg); err != nil {
							logger.Default().Warn("set ssh config in import", zap.Error(err))
						}
					}
				case a.IsDatabase() && a.Config != "":
					cfg, err := a.GetDatabaseConfig()
					if err == nil {
						if cfg.SSHAssetID > 0 {
							oldSSHAssetID = cfg.SSHAssetID
							cfg.SSHAssetID = 0
						}
						if data.IncludesCredentials && cfg.Password != "" && crypto != nil {
							encrypted, encErr := crypto.Encrypt(cfg.Password)
							if encErr != nil {
								logger.Default().Warn("re-encrypt db password", zap.Error(encErr))
							} else {
								cfg.Password = encrypted
							}
						}
						if err := a.SetDatabaseConfig(cfg); err != nil {
							logger.Default().Warn("set db config in import", zap.Error(err))
						}
					}
				case a.IsRedis() && a.Config != "":
					cfg, err := a.GetRedisConfig()
					if err == nil {
						if cfg.SSHAssetID > 0 {
							oldSSHAssetID = cfg.SSHAssetID
							cfg.SSHAssetID = 0
						}
						if data.IncludesCredentials && cfg.Password != "" && crypto != nil {
							encrypted, encErr := crypto.Encrypt(cfg.Password)
							if encErr != nil {
								logger.Default().Warn("re-encrypt redis password", zap.Error(encErr))
							} else {
								cfg.Password = encrypted
							}
						}
						if err := a.SetRedisConfig(cfg); err != nil {
							logger.Default().Warn("set redis config in import", zap.Error(err))
						}
					}
				}

				if err := tx.Create(a).Error; err != nil {
					return fmt.Errorf("创建资产 %s 失败: %w", a.Name, err)
				}
				assetIDMap[oldID] = a.ID
				result.AssetsImported++

				if oldJumpHostID > 0 {
					deferredRefs = append(deferredRefs, deferredRef{a.ID, oldJumpHostID, "jump_host"})
				}
				if oldSSHAssetID > 0 {
					deferredRefs = append(deferredRefs, deferredRef{a.ID, oldSSHAssetID, "ssh_tunnel"})
				}
			}

			// 回填跳板机和 SSH 隧道引用
			for _, ref := range deferredRefs {
				newRefID, ok := assetIDMap[ref.oldRefID]
				if !ok {
					continue
				}
				var asset asset_entity.Asset
				if err := tx.Where("id = ?", ref.newAssetID).First(&asset).Error; err != nil {
					continue
				}
				switch ref.refType {
				case "jump_host":
					cfg, err := asset.GetSSHConfig()
					if err != nil {
						continue
					}
					cfg.JumpHostID = newRefID
					if err := asset.SetSSHConfig(cfg); err != nil {
						continue
					}
				case "ssh_tunnel":
					if asset.IsDatabase() {
						cfg, err := asset.GetDatabaseConfig()
						if err != nil {
							continue
						}
						cfg.SSHAssetID = newRefID
						if err := asset.SetDatabaseConfig(cfg); err != nil {
							continue
						}
					} else if asset.IsRedis() {
						cfg, err := asset.GetRedisConfig()
						if err != nil {
							continue
						}
						cfg.SSHAssetID = newRefID
						if err := asset.SetRedisConfig(cfg); err != nil {
							continue
						}
					}
				}
				if err := tx.Save(&asset).Error; err != nil {
					return fmt.Errorf("更新资产引用失败: %w", err)
				}
			}
		}

		// 5. 端口转发
		if opts.ImportForwards && len(data.Forwards) > 0 {
			if isReplace {
				if err := tx.Exec("DELETE FROM forward_rules").Error; err != nil {
					return fmt.Errorf("清除转发规则失败: %w", err)
				}
				if err := tx.Exec("DELETE FROM forward_configs").Error; err != nil {
					return fmt.Errorf("清除转发配置失败: %w", err)
				}
			}
			for _, bf := range data.Forwards {
				newAssetID, ok := assetIDMap[bf.AssetID]
				if !ok {
					// 合并模式下资产可能已存在，尝试按名字匹配
					continue
				}
				config := bf.ForwardConfig
				config.ID = 0
				config.AssetID = newAssetID
				if err := tx.Create(&config).Error; err != nil {
					return fmt.Errorf("创建转发配置 %s 失败: %w", config.Name, err)
				}
				for _, rule := range bf.Rules {
					rule.ID = 0
					rule.ConfigID = config.ID
					if err := tx.Create(rule).Error; err != nil {
						return fmt.Errorf("创建转发规则失败: %w", err)
					}
				}
				result.ForwardsImported++
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// 6. 客户端设置透传
	if opts.ImportShortcuts && len(data.Shortcuts) > 0 {
		result.Shortcuts = string(data.Shortcuts)
	}
	if opts.ImportThemes && len(data.CustomThemes) > 0 {
		result.CustomThemes = string(data.CustomThemes)
	}

	return result, nil
}

// --- 内部辅助函数 ---

// resolveDependentAssets 递归补全跳板机和 SSH 隧道资产
func resolveDependentAssets(selectedIDs map[int64]bool, assetMap map[int64]*asset_entity.Asset) {
	changed := true
	for changed {
		changed = false
		for id := range selectedIDs {
			a, ok := assetMap[id]
			if !ok {
				continue
			}
			switch {
			case a.IsSSH() && a.Config != "":
				cfg, err := a.GetSSHConfig()
				if err == nil && cfg.JumpHostID > 0 && !selectedIDs[cfg.JumpHostID] {
					selectedIDs[cfg.JumpHostID] = true
					changed = true
				}
			case a.IsDatabase() && a.Config != "":
				cfg, err := a.GetDatabaseConfig()
				if err == nil && cfg.SSHAssetID > 0 && !selectedIDs[cfg.SSHAssetID] {
					selectedIDs[cfg.SSHAssetID] = true
					changed = true
				}
			case a.IsRedis() && a.Config != "":
				cfg, err := a.GetRedisConfig()
				if err == nil && cfg.SSHAssetID > 0 && !selectedIDs[cfg.SSHAssetID] {
					selectedIDs[cfg.SSHAssetID] = true
					changed = true
				}
			}
		}
	}
}

// collectAncestorGroups 收集分组及其所有祖先
func collectAncestorGroups(groupID int64, groupMap map[int64]*group_entity.Group, result map[int64]bool) {
	for groupID > 0 && !result[groupID] {
		result[groupID] = true
		g, ok := groupMap[groupID]
		if !ok {
			break
		}
		groupID = g.ParentID
	}
}

// collectPolicyGroupIDs 从资产和分组中收集用户自定义策略组 ID（ID>0）
func collectPolicyGroupIDs(assets []*asset_entity.Asset, groups []*group_entity.Group) map[int64]bool {
	ids := make(map[int64]bool)
	for _, a := range assets {
		collectPolicyIDs(a.CmdPolicy, ids)
	}
	for _, g := range groups {
		collectPolicyIDs(g.CmdPolicy, ids)
		collectPolicyIDs(g.QryPolicy, ids)
		collectPolicyIDs(g.RdsPolicy, ids)
	}
	return ids
}

// collectPolicyIDs 从策略 JSON 中提取 Groups 字段的 ID
func collectPolicyIDs(policyJSON string, ids map[int64]bool) {
	if policyJSON == "" {
		return
	}
	// 尝试解析为含 Groups 字段的结构
	var p struct {
		Groups []int64 `json:"groups"`
	}
	if err := json.Unmarshal([]byte(policyJSON), &p); err != nil {
		return
	}
	for _, id := range p.Groups {
		if !policy_group_entity.IsBuiltinID(id) {
			ids[id] = true
		}
	}
}

// exportCredentials 导出关联凭据（解密为明文）
func exportCredentials(ctx context.Context, assets []*asset_entity.Asset, crypto CredentialCrypto) ([]*BackupCredential, error) {
	credIDs := make(map[int64]bool)
	for _, a := range assets {
		if a.IsSSH() && a.Config != "" {
			cfg, err := a.GetSSHConfig()
			if err == nil && cfg.CredentialID > 0 {
				credIDs[cfg.CredentialID] = true
			}
		}
	}
	if len(credIDs) == 0 {
		return nil, nil
	}

	var result []*BackupCredential
	for credID := range credIDs {
		cred, err := credential_repo.Credential().Find(ctx, credID)
		if err != nil {
			logger.Default().Warn("credential not found during export", zap.Int64("id", credID), zap.Error(err))
			continue
		}
		bc := &BackupCredential{Credential: *cred}
		if cred.Password != "" {
			plain, err := crypto.Decrypt(cred.Password)
			if err != nil {
				return nil, fmt.Errorf("解密凭据 %s 密码失败: %w", cred.Name, err)
			}
			bc.PlainPassword = plain
			bc.Password = "" // 清除密文
		}
		if cred.PrivateKey != "" {
			plain, err := crypto.Decrypt(cred.PrivateKey)
			if err != nil {
				return nil, fmt.Errorf("解密凭据 %s 私钥失败: %w", cred.Name, err)
			}
			bc.PlainPrivateKey = plain
			bc.PrivateKey = "" // 清除密文
		}
		result = append(result, bc)
	}
	return result, nil
}

// decryptAssetPasswords 解密资产 Config 中的内联密码为明文
func decryptAssetPasswords(assets []*asset_entity.Asset, crypto CredentialCrypto) error {
	for _, a := range assets {
		switch {
		case a.IsSSH() && a.Config != "":
			cfg, err := a.GetSSHConfig()
			if err != nil {
				continue
			}
			changed := false
			if cfg.Password != "" {
				plain, err := crypto.Decrypt(cfg.Password)
				if err != nil {
					return fmt.Errorf("解密资产 %s SSH 密码失败: %w", a.Name, err)
				}
				cfg.Password = plain
				changed = true
			}
			if cfg.Proxy != nil && cfg.Proxy.Password != "" {
				plain, err := crypto.Decrypt(cfg.Proxy.Password)
				if err != nil {
					return fmt.Errorf("解密资产 %s 代理密码失败: %w", a.Name, err)
				}
				cfg.Proxy.Password = plain
				changed = true
			}
			if changed {
				if err := a.SetSSHConfig(cfg); err != nil {
					return err
				}
			}
		case a.IsDatabase() && a.Config != "":
			cfg, err := a.GetDatabaseConfig()
			if err != nil {
				continue
			}
			if cfg.Password != "" {
				plain, err := crypto.Decrypt(cfg.Password)
				if err != nil {
					return fmt.Errorf("解密资产 %s 数据库密码失败: %w", a.Name, err)
				}
				cfg.Password = plain
				if err := a.SetDatabaseConfig(cfg); err != nil {
					return err
				}
			}
		case a.IsRedis() && a.Config != "":
			cfg, err := a.GetRedisConfig()
			if err != nil {
				continue
			}
			if cfg.Password != "" {
				plain, err := crypto.Decrypt(cfg.Password)
				if err != nil {
					return fmt.Errorf("解密资产 %s Redis 密码失败: %w", a.Name, err)
				}
				cfg.Password = plain
				if err := a.SetRedisConfig(cfg); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// stripAssetSecrets 清除资产配置中的敏感字段
func stripAssetSecrets(assets []*asset_entity.Asset) {
	for _, a := range assets {
		switch {
		case a.IsSSH() && a.Config != "":
			cfg, err := a.GetSSHConfig()
			if err != nil {
				continue
			}
			cfg.Password = ""
			cfg.CredentialID = 0
			cfg.PrivateKeys = nil
			if cfg.Proxy != nil {
				cfg.Proxy.Password = ""
			}
			if err := a.SetSSHConfig(cfg); err != nil {
				logger.Default().Warn("strip ssh secrets", zap.Error(err))
			}
		case a.IsDatabase() && a.Config != "":
			cfg, err := a.GetDatabaseConfig()
			if err != nil {
				continue
			}
			cfg.Password = ""
			if err := a.SetDatabaseConfig(cfg); err != nil {
				logger.Default().Warn("strip db secrets", zap.Error(err))
			}
		case a.IsRedis() && a.Config != "":
			cfg, err := a.GetRedisConfig()
			if err != nil {
				continue
			}
			cfg.Password = ""
			if err := a.SetRedisConfig(cfg); err != nil {
				logger.Default().Warn("strip redis secrets", zap.Error(err))
			}
		}
	}
}

// exportForwards 导出关联的端口转发配置
func exportForwards(ctx context.Context, assetIDs map[int64]bool) ([]*BackupForward, error) {
	configs, err := forward_repo.Forward().ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	var result []*BackupForward
	for _, config := range configs {
		if !assetIDs[config.AssetID] {
			continue
		}
		rules, err := forward_repo.Forward().ListRulesByConfigID(ctx, config.ID)
		if err != nil {
			return nil, fmt.Errorf("导出转发规则失败: %w", err)
		}
		result = append(result, &BackupForward{
			ForwardConfig: *config,
			Rules:         rules,
		})
	}
	return result, nil
}

// remapGroupPolicyGroupIDs 回填分组中策略的 Groups 引用
func remapGroupPolicyGroupIDs(g *group_entity.Group, pgIDMap map[int64]int64) {
	g.CmdPolicy = remapPolicyGroupRefs(g.CmdPolicy, pgIDMap)
	g.QryPolicy = remapPolicyGroupRefs(g.QryPolicy, pgIDMap)
	g.RdsPolicy = remapPolicyGroupRefs(g.RdsPolicy, pgIDMap)
}

// remapAssetPolicyGroupIDs 回填资产中策略的 Groups 引用
func remapAssetPolicyGroupIDs(a *asset_entity.Asset, pgIDMap map[int64]int64) {
	a.CmdPolicy = remapPolicyGroupRefs(a.CmdPolicy, pgIDMap)
}

// remapPolicyGroupRefs 替换策略 JSON 中的 groups ID 引用
func remapPolicyGroupRefs(policyJSON string, pgIDMap map[int64]int64) string {
	if policyJSON == "" || len(pgIDMap) == 0 {
		return policyJSON
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(policyJSON), &raw); err != nil {
		return policyJSON
	}
	groupsRaw, ok := raw["groups"]
	if !ok {
		return policyJSON
	}
	var groups []int64
	if err := json.Unmarshal(groupsRaw, &groups); err != nil {
		return policyJSON
	}
	changed := false
	for i, id := range groups {
		if policy_group_entity.IsBuiltinID(id) {
			continue
		}
		if newID, ok := pgIDMap[id]; ok {
			groups[i] = newID
			changed = true
		}
	}
	if !changed {
		return policyJSON
	}
	newGroupsRaw, err := json.Marshal(groups)
	if err != nil {
		return policyJSON
	}
	raw["groups"] = newGroupsRaw
	result, err := json.Marshal(raw)
	if err != nil {
		return policyJSON
	}
	return string(result)
}

// sortGroups 拓扑排序分组，确保父分组在子分组之前
func sortGroups(groups []*group_entity.Group) []*group_entity.Group {
	sorted := make([]*group_entity.Group, 0, len(groups))
	added := make(map[int64]bool)

	for len(sorted) < len(groups) {
		progress := false
		for _, g := range groups {
			if added[g.ID] {
				continue
			}
			if g.ParentID == 0 || added[g.ParentID] {
				sorted = append(sorted, g)
				added[g.ID] = true
				progress = true
			}
		}
		if !progress {
			for _, g := range groups {
				if !added[g.ID] {
					sorted = append(sorted, g)
				}
			}
			break
		}
	}
	return sorted
}
