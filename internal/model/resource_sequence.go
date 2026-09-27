package model

type ResourceSequence struct {
	Name      string `gorm:"primaryKey;size:128" json:"name"`
	NextValue int    `gorm:"not null;default:1" json:"next_value"`
	FullAudit
}

func (ResourceSequence) TableName() string {
	return "resource_sequences"
}
