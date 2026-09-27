package dbproxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestPostgresStartupNegotiatesProtocol32To30(t *testing.T) {
	message := postgresStartupMessageForVersion(
		3,
		2,
		[][2]string{
			{"user", "gateway-user"},
			{"database", "appdb"},
			{"_pq_.example", "required"},
			{"application_name", "psql18"},
		},
	)
	startup, err := parsePostgresStartup(message)
	if err != nil {
		t.Fatal(err)
	}
	if startup.protocolMinor != 2 {
		t.Fatalf("protocol minor = %d, want 2", startup.protocolMinor)
	}
	if len(startup.unsupportedOptions) != 1 ||
		startup.unsupportedOptions[0] != "_pq_.example" {
		t.Fatalf("unsupported options = %v", startup.unsupportedOptions)
	}

	var output bytes.Buffer
	if err := writePostgresProtocolNegotiation(startup, &output); err != nil {
		t.Fatal(err)
	}
	raw := output.Bytes()
	if len(raw) < 13 || raw[0] != 'v' {
		t.Fatalf("NegotiateProtocolVersion = %x", raw)
	}
	if got := binary.BigEndian.Uint32(raw[5:9]); got != postgresSupportedProtocolMinor {
		t.Fatalf("negotiated protocol minor = %d", got)
	}
	if got := binary.BigEndian.Uint32(raw[9:13]); got != 1 {
		t.Fatalf("unsupported option count = %d", got)
	}
	if got := string(raw[13:]); got != "_pq_.example\x00" {
		t.Fatalf("unsupported option payload = %q", got)
	}
}

func TestPostgresUpstreamStartupPreservesRuntimeParameters(t *testing.T) {
	clientMessage := postgresStartupMessageForVersion(
		3,
		2,
		[][2]string{
			{"user", "gateway-user"},
			{"database", " app db "},
			{"application_name", "psql18"},
			{"options", "-c search_path=tenant"},
			{"_pq_.example", "required"},
		},
	)
	clientStartup, err := parsePostgresStartup(clientMessage)
	if err != nil {
		t.Fatal(err)
	}
	upstreamMessage := buildPostgresUpstreamStartupMessage("upstream-user", clientStartup)
	upstreamStartup, err := parsePostgresStartup(upstreamMessage)
	if err != nil {
		t.Fatal(err)
	}
	if upstreamStartup.protocolMinor != 0 {
		t.Fatalf("upstream protocol minor = %d, want 0", upstreamStartup.protocolMinor)
	}
	if upstreamStartup.username != "upstream-user" {
		t.Fatalf("upstream user = %q", upstreamStartup.username)
	}
	if upstreamStartup.database != " app db " {
		t.Fatalf("upstream database = %q, want exact client database", upstreamStartup.database)
	}
	parameters := postgresStartupParameterMap(upstreamStartup)
	if parameters["application_name"] != "psql18" ||
		parameters["options"] != "-c search_path=tenant" {
		t.Fatalf("forwarded runtime parameters = %v", parameters)
	}
	if _, exists := parameters["_pq_.example"]; exists {
		t.Fatal("unsupported protocol option was forwarded upstream")
	}
}

func TestWritePostgresBytesCompletesShortWrites(t *testing.T) {
	writer := &postgresShortWriter{limit: 2}
	if err := writePostgresBytes(writer, []byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if got := writer.output.String(); got != "abcdef" {
		t.Fatalf("short-write output = %q", got)
	}

	writer = &postgresShortWriter{}
	if err := writePostgresBytes(writer, []byte("x")); err != io.ErrShortWrite {
		t.Fatalf("zero-byte write error = %v, want io.ErrShortWrite", err)
	}
}

func postgresStartupMessageForVersion(
	major, minor uint32,
	parameters [][2]string,
) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, major<<16|minor)
	for _, parameter := range parameters {
		payload = append(payload, parameter[0]...)
		payload = append(payload, 0)
		payload = append(payload, parameter[1]...)
		payload = append(payload, 0)
	}
	payload = append(payload, 0)
	message := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(message[:4], uint32(len(message)))
	copy(message[4:], payload)
	return message
}

func postgresStartupParameterMap(startup postgresStartup) map[string]string {
	parameters := make(map[string]string)
	for _, parameter := range startup.parameters {
		parameters[parameter.name] = parameter.value
	}
	return parameters
}

type postgresShortWriter struct {
	limit  int
	output bytes.Buffer
}

func (writer *postgresShortWriter) Write(data []byte) (int, error) {
	if writer.limit == 0 {
		return 0, nil
	}
	if len(data) > writer.limit {
		data = data[:writer.limit]
	}
	return writer.output.Write(data)
}
