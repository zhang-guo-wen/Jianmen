package webrdp

import (
	"fmt"
	"strconv"

	"jianmen/internal/proxy/rdpproxy"
)

const (
	maxTrackedChannelStreams = 256
	maxChannelAuditEvents    = 4096
)

func validateChannelInstruction(
	instruction rdpproxy.Instruction,
) error {
	wantArgs := map[string]int{
		"clipboard":  2,
		"file":       3,
		"put":        4,
		"get":        2,
		"body":       4,
		"filesystem": 2,
	}[instruction.Opcode]
	if len(instruction.Args) != wantArgs {
		return fmt.Errorf(
			"RDP %s instruction has %d arguments; want %d",
			instruction.Opcode,
			len(instruction.Args),
			wantArgs,
		)
	}

	switch instruction.Opcode {
	case "put", "body":
		if err := validateGuacamoleIndex(
			instruction.Opcode+" object",
			instruction.Args[0],
		); err != nil {
			return err
		}
		return validateGuacamoleIndex(
			instruction.Opcode+" stream",
			instruction.Args[1],
		)
	case "get", "filesystem":
		return validateGuacamoleIndex(
			instruction.Opcode+" object",
			instruction.Args[0],
		)
	default:
		return validateGuacamoleIndex(
			instruction.Opcode+" stream",
			instruction.Args[0],
		)
	}
}

func validateGuacamoleIndex(name string, raw string) error {
	index, err := strconv.ParseUint(raw, 10, 31)
	if err != nil || raw != strconv.FormatUint(index, 10) {
		return fmt.Errorf(
			"RDP %s ID must be a canonical non-negative 31-bit integer",
			name,
		)
	}
	return nil
}
