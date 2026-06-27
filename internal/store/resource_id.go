package store

import (
	"jianmen/internal/model"
)

// nextHostResourceSeq 获取主机账号下一个自增序号
func (s *DBStore) nextHostResourceSeq() (int, error) {
	var maxSeq int
	if err := s.db.Model(&model.HostAccount{}).
		Select("COALESCE(MAX(resource_seq), 0)").Scan(&maxSeq).Error; err != nil {
		return 0, err
	}
	return maxSeq + 1, nil
}

// nextDBResourceSeq 获取数据库账号下一个自增序号
func (s *DBStore) nextDBResourceSeq() (int, error) {
	var maxSeq int
	if err := s.db.Model(&model.DatabaseAccount{}).
		Select("COALESCE(MAX(resource_seq), 0)").Scan(&maxSeq).Error; err != nil {
		return 0, err
	}
	return maxSeq + 1, nil
}
