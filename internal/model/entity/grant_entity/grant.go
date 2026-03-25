package grant_entity

// 授权状态
const (
	GrantStatusPending  = 1
	GrantStatusApproved = 2
	GrantStatusRejected = 3
)

// GrantSession 批量审批授权
type GrantSession struct {
	ID          string `gorm:"column:id;primaryKey;type:varchar(36)"` // UUIDv4
	Description string `gorm:"column:description;type:text"`
	Status      int    `gorm:"column:status;not null;default:1"`
	Createtime  int64  `gorm:"column:createtime;not null"`
	Updatetime  int64  `gorm:"column:updatetime"`
}

// TableName GORM 表名
func (GrantSession) TableName() string {
	return "grant_sessions"
}

// GrantItem 授权中的单条操作
type GrantItem struct {
	ID             int64  `gorm:"column:id;primaryKey;autoIncrement"`
	GrantSessionID string `gorm:"column:grant_session_id;type:varchar(36);index;not null"`
	ItemIndex      int    `gorm:"column:item_index;not null"`
	ToolName       string `gorm:"column:tool_name;type:varchar(100);not null"` // "exec", "cp", "create", "update"
	AssetID        int64  `gorm:"column:asset_id;default:0"`
	AssetName      string `gorm:"column:asset_name;type:varchar(255)"`
	GroupID        int64  `gorm:"column:group_id;default:0"`
	GroupName      string `gorm:"column:group_name;type:varchar(255)"`
	Command        string `gorm:"column:command;type:text"`
	Detail         string `gorm:"column:detail;type:text"`
	Createtime     int64  `gorm:"column:createtime"`
}

// TableName GORM 表名
func (GrantItem) TableName() string {
	return "grant_items"
}
