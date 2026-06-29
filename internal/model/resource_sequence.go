package model

import "time"

type ResourceSequence struct {
	Name      string    `gorm:"primaryKey;size:128" json:"name"`
	NextValue int       `gorm:"not null;default:1" json:"next_value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ResourceSequence) TableName() string {
	return "resource_sequences"
}
