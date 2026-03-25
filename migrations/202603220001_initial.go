package migrations

import (
	"github.com/opskat/opskat/internal/model/entity/asset_entity"
	"github.com/opskat/opskat/internal/model/entity/audit_entity"
	"github.com/opskat/opskat/internal/model/entity/conversation_entity"
	"github.com/opskat/opskat/internal/model/entity/credential_entity"
	"github.com/opskat/opskat/internal/model/entity/forward_entity"
	"github.com/opskat/opskat/internal/model/entity/grant_entity"
	"github.com/opskat/opskat/internal/model/entity/group_entity"
	"github.com/opskat/opskat/internal/model/entity/host_key_entity"
	"github.com/opskat/opskat/internal/model/entity/policy_group_entity"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202603220001 初始化所有表
func migration202603220001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202603220001",
		Migrate: func(tx *gorm.DB) error {
			return tx.AutoMigrate(
				&asset_entity.Asset{},
				&group_entity.Group{},
				&conversation_entity.Conversation{},
				&conversation_entity.Message{},
				&audit_entity.AuditLog{},
				&grant_entity.GrantSession{},
				&grant_entity.GrantItem{},
				&forward_entity.ForwardConfig{},
				&forward_entity.ForwardRule{},
				&credential_entity.Credential{},
				&host_key_entity.HostKey{},
				&policy_group_entity.PolicyGroup{},
			)
		},
		Rollback: func(tx *gorm.DB) error {
			tables := []string{
				"host_keys",
				"credentials",
				"forward_rules",
				"forward_configs",
				"grant_items",
				"grant_sessions",
				"audit_logs",
				"conversation_messages",
				"conversations",
				"groups",
				"assets",
			}
			for _, table := range tables {
				if err := tx.Migrator().DropTable(table); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
