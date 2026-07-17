package model

import "time"

const (
	ContainerRuntimeDocker     = "docker"
	ContainerRuntimeContainerd = "containerd"

	ContainerConnectionSSH        = "ssh"
	ContainerConnectionDockerAPI  = "docker_api"
	ContainerConnectionContainerd = "containerd"
)

// ContainerEndpoint stores a managed Docker or containerd connection.
type ContainerEndpoint struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	Name           string    `gorm:"size:255;not null" json:"name"`
	GroupName      string    `gorm:"size:128" json:"group"`
	Runtime        string    `gorm:"index;size:32;not null" json:"runtime"`
	ConnectionMode string    `gorm:"index;size:32;not null" json:"connection_mode"`
	Address        string    `gorm:"size:2048;not null" json:"address"`
	Port           int       `gorm:"not null;default:0" json:"port"`
	HostID         string    `gorm:"index;size:64" json:"host_id,omitempty"`
	HostAccountID  string    `gorm:"index;size:64" json:"host_account_id,omitempty"`
	Remark         string    `gorm:"type:text" json:"remark,omitempty"`
	Status         string    `gorm:"index;size:32;not null;default:active" json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
