package auditartifact

import (
	"errors"
	"fmt"
	"io"
	"os"
)

var ErrSectionUnavailable = errors.New("audit artifact section unavailable")

func OpenSection(path string, section Section) (*os.File, *io.SectionReader, error) {
	if section.Offset < 0 || section.Bytes < 0 {
		return nil, nil, ErrSectionUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrSectionUnavailable, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("%w: %v", ErrSectionUnavailable, err)
	}
	if section.Offset > info.Size() || section.Bytes > info.Size()-section.Offset {
		_ = file.Close()
		return nil, nil, ErrSectionUnavailable
	}
	return file, io.NewSectionReader(file, section.Offset, section.Bytes), nil
}
