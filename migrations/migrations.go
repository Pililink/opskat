package migrations

import (
	"ops-cat/internal/model/entity/asset_entity"
	"ops-cat/internal/model/entity/audit_entity"
	"ops-cat/internal/model/entity/conversation_entity"
	"ops-cat/internal/model/entity/forward_entity"
	"ops-cat/internal/model/entity/group_entity"
	"ops-cat/internal/model/entity/plan_entity"
	"ops-cat/internal/model/entity/credential_entity"
	"ops-cat/internal/model/entity/ssh_key_entity"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// RunMigrations 执行数据库迁移
func RunMigrations(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "202603220001",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&asset_entity.Asset{}); err != nil {
					return err
				}
				if err := tx.AutoMigrate(&group_entity.Group{}); err != nil {
					return err
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				if err := tx.Migrator().DropTable("assets"); err != nil {
					return err
				}
				return tx.Migrator().DropTable("groups")
			},
		},
		{
			ID: "202603220002",
			Migrate: func(tx *gorm.DB) error {
				// 添加 icon 列到 assets 和 groups 表
				if err := tx.AutoMigrate(&asset_entity.Asset{}); err != nil {
					return err
				}
				if err := tx.AutoMigrate(&group_entity.Group{}); err != nil {
					return err
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				if err := tx.Migrator().DropColumn("assets", "icon"); err != nil {
					return err
				}
				return tx.Migrator().DropColumn("groups", "icon")
			},
		},
		{
			ID: "202603220003",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&ssh_key_entity.SSHKey{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("ssh_keys")
			},
		},
		{
			ID: "202603220004",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&ssh_key_entity.SSHKey{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropColumn("ssh_keys", "comment")
			},
		},
		{
			ID: "202603230001",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&conversation_entity.Conversation{}); err != nil {
					return err
				}
				return tx.AutoMigrate(&conversation_entity.Message{})
			},
			Rollback: func(tx *gorm.DB) error {
				if err := tx.Migrator().DropTable("conversation_messages"); err != nil {
					return err
				}
				return tx.Migrator().DropTable("conversations")
			},
		},
		{
			ID: "202603240001",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&asset_entity.Asset{}); err != nil {
					return err
				}
				return tx.AutoMigrate(&group_entity.Group{})
			},
			Rollback: func(tx *gorm.DB) error {
				if err := tx.Migrator().DropColumn("assets", "command_policy"); err != nil {
					return err
				}
				return tx.Migrator().DropColumn("groups", "command_policy")
			},
		},
		{
			ID: "202603240002",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&audit_entity.AuditLog{}); err != nil {
					return err
				}
				if err := tx.AutoMigrate(&plan_entity.PlanSession{}); err != nil {
					return err
				}
				return tx.AutoMigrate(&plan_entity.PlanItem{})
			},
			Rollback: func(tx *gorm.DB) error {
				if err := tx.Migrator().DropTable("plan_items"); err != nil {
					return err
				}
				if err := tx.Migrator().DropTable("plan_sessions"); err != nil {
					return err
				}
				return tx.Migrator().DropTable("audit_logs")
			},
		},
		{
			ID: "202603250001",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.AutoMigrate(&forward_entity.ForwardConfig{}); err != nil {
					return err
				}
				return tx.AutoMigrate(&forward_entity.ForwardRule{})
			},
			Rollback: func(tx *gorm.DB) error {
				if err := tx.Migrator().DropTable("forward_rules"); err != nil {
					return err
				}
				return tx.Migrator().DropTable("forward_configs")
			},
		},
		{
			ID: "202603250002",
			Migrate: func(tx *gorm.DB) error {
				return tx.AutoMigrate(&group_entity.Group{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropColumn("groups", "description")
			},
		},
		{
			ID: "202603250003",
			Migrate: func(tx *gorm.DB) error {
				// 创建 credentials 统一凭证表
				if err := tx.AutoMigrate(&credential_entity.Credential{}); err != nil {
					return err
				}
				// 迁移 ssh_keys 数据到 credentials
				if tx.Migrator().HasTable("ssh_keys") {
					if err := tx.Exec(`
						INSERT INTO credentials (id, name, type, comment, key_type, key_size, private_key, public_key, fingerprint, createtime, updatetime)
						SELECT id, name, 'ssh_key', comment, key_type, key_size, private_key, public_key, fingerprint, createtime, updatetime
						FROM ssh_keys
					`).Error; err != nil {
						return err
					}
				}
				// 迁移资产中的 key_id → credential_id
				if err := tx.Exec(`
					UPDATE assets SET config = json_set(config, '$.credential_id', json_extract(config, '$.key_id'))
					WHERE status = 1 AND json_extract(config, '$.key_id') IS NOT NULL AND json_extract(config, '$.key_id') > 0
				`).Error; err != nil {
					return err
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable("credentials")
			},
		},
	})
	return m.Migrate()
}
