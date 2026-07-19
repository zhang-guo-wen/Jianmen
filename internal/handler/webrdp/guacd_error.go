package webrdp

import (
	"errors"
	"fmt"
	"strconv"

	"jianmen/internal/proxy/rdpproxy"
)

type guacdProtocolError struct {
	status int
}

func (e *guacdProtocolError) Error() string {
	return fmt.Sprintf("guacd closed the RDP session with status %d", e.status)
}

func guacdInstructionError(instruction rdpproxy.Instruction) error {
	if instruction.Opcode != "error" {
		return nil
	}
	if len(instruction.Args) != 2 {
		return errors.New("guacd sent a malformed error instruction")
	}
	status, err := strconv.Atoi(instruction.Args[1])
	if err != nil || status < 0 {
		return errors.New("guacd sent an invalid error status")
	}
	return &guacdProtocolError{status: status}
}
