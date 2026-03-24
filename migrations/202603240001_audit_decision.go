package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202603240001 审计日志添加决策信息字段
func migration202603240001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202603240001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec("ALTER TABLE audit_logs ADD COLUMN session_id VARCHAR(64) DEFAULT ''").Error; err != nil {
				return err
			}
			if err := tx.Exec("ALTER TABLE audit_logs ADD COLUMN decision VARCHAR(10) DEFAULT ''").Error; err != nil {
				return err
			}
			if err := tx.Exec("ALTER TABLE audit_logs ADD COLUMN decision_source VARCHAR(30) DEFAULT ''").Error; err != nil {
				return err
			}
			if err := tx.Exec("ALTER TABLE audit_logs ADD COLUMN matched_pattern VARCHAR(500) DEFAULT ''").Error; err != nil {
				return err
			}
			return nil
		},
	}
}
