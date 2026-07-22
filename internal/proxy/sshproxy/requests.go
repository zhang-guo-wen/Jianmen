package sshproxy

import "golang.org/x/crypto/ssh"

type ptyRequest struct {
	Term     string
	Columns  uint32
	Rows     uint32
	WidthPx  uint32
	HeightPx uint32
	Modes    string
}

type windowChangeRequest struct {
	Columns  uint32
	Rows     uint32
	WidthPx  uint32
	HeightPx uint32
}

type subsystemRequest struct {
	Name string
}

type execRequest struct {
	Command string
}

func parsePtyRequest(payload []byte) (ptyRequest, bool) {
	var req ptyRequest
	if err := ssh.Unmarshal(payload, &req); err != nil {
		return ptyRequest{}, false
	}
	return req, true
}

func parseWindowChange(payload []byte) (windowChangeRequest, bool) {
	var req windowChangeRequest
	if err := ssh.Unmarshal(payload, &req); err != nil {
		return windowChangeRequest{}, false
	}
	return req, true
}

func parseSubsystemName(payload []byte) string {
	var req subsystemRequest
	if err := ssh.Unmarshal(payload, &req); err != nil {
		return ""
	}
	return req.Name
}

func parseExecRequest(payload []byte) (execRequest, bool) {
	var req execRequest
	if err := ssh.Unmarshal(payload, &req); err != nil {
		return execRequest{}, false
	}
	return req, true
}
