package dbproxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

func TestRunPostgresSCRAMUsesCorrectFramesAndVerifiesServer(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	serverResult := make(chan error, 1)
	go func() {
		serverResult <- servePostgresSCRAMTest(server, postgresSCRAMTestOptions{})
	}()

	err := runPostgresSCRAMWithNonce(
		client,
		"probe",
		"secret",
		[]byte("SCRAM-SHA-256\x00\x00"),
		"client-nonce",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSCRAMRequiresExplicitlyOfferedMechanism(t *testing.T) {
	if postgresSCRAMMechanismOffered([]byte("OTHER-SASL\x00\x00"), postgresSCRAMSHA256) {
		t.Fatal("SCRAM-SHA-256 was accepted when the server did not offer it")
	}
	if !postgresSCRAMMechanismOffered([]byte("OTHER-SASL\x00SCRAM-SHA-256\x00\x00"), postgresSCRAMSHA256) {
		t.Fatal("SCRAM-SHA-256 was not found in a valid mechanism list")
	}
}

func TestPostgresSCRAMNormalizesUnicodePasswordLikePostgreSQL(t *testing.T) {
	const password = "before\u00a0after"
	normalized := normalizePostgresSCRAMPassword(password)
	if string(normalized) != "before after" {
		t.Fatalf("normalized password = %q", normalized)
	}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- servePostgresSCRAMTest(
			server,
			postgresSCRAMTestOptions{password: password},
		)
	}()
	if err := runPostgresSCRAMWithNonce(
		client,
		"probe",
		password,
		[]byte("SCRAM-SHA-256\x00\x00"),
		"client-nonce",
	); err != nil {
		t.Fatal(err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSCRAMUsesRFC4013SASLprep(t *testing.T) {
	if got := string(normalizePostgresSCRAMPassword("pass\u2163")); got != "passIV" {
		t.Fatalf("SASLprep compatibility normalization = %q", got)
	}
	if got := string(normalizePostgresSCRAMPassword("before\tafter")); got != "before\tafter" {
		t.Fatalf("invalid SASLprep input fallback = %q", got)
	}
}

func TestRunPostgresSCRAMRejectsInvalidServerProofAndParameters(t *testing.T) {
	tests := []struct {
		name      string
		options   postgresSCRAMTestOptions
		wantError string
	}{
		{
			name:      "server signature mismatch",
			options:   postgresSCRAMTestOptions{badVerifier: true},
			wantError: "signature",
		},
		{
			name:      "iteration count too high",
			options:   postgresSCRAMTestOptions{iterations: maxPostgresSCRAMIterations + 1},
			wantError: "iteration",
		},
		{
			name:      "iteration count too low",
			options:   postgresSCRAMTestOptions{iterations: minPostgresSCRAMIterations - 1},
			wantError: "iteration",
		},
		{
			name:      "nonce mismatch",
			options:   postgresSCRAMTestOptions{nonceMismatch: true},
			wantError: "nonce",
		},
		{
			name:      "server final error",
			options:   postgresSCRAMTestOptions{serverError: true},
			wantError: "server rejected",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			serverResult := make(chan error, 1)
			go func() {
				serverResult <- servePostgresSCRAMTest(server, test.options)
			}()

			err := runPostgresSCRAMWithNonce(
				client,
				"probe",
				"secret",
				[]byte("SCRAM-SHA-256\x00\x00"),
				"client-nonce",
			)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.wantError) {
				t.Fatalf("runPostgresSCRAMWithNonce() error = %v, want %q", err, test.wantError)
			}
			if serverErr := <-serverResult; serverErr != nil {
				t.Fatal(serverErr)
			}
		})
	}
}

func TestRunPostgresSCRAMRejectsMalformedAndTruncatedFramesWithoutPanic(t *testing.T) {
	t.Run("truncated continue", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		go func() {
			defer server.Close()
			_, _, _ = readPostgresTestSASLInitial(server)
			raw := postgresTestMessage('R', append([]byte{0, 0, 0, 11}, []byte("r=incomplete")...))
			_, _ = server.Write(raw[:7])
		}()
		if err := runPostgresSCRAMWithNonce(client, "probe", "secret", []byte("SCRAM-SHA-256\x00\x00"), "client-nonce"); err == nil {
			t.Fatal("truncated PostgreSQL SCRAM frame was accepted")
		}
	})

	t.Run("oversized continue", func(t *testing.T) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		go func() {
			_, _, _ = readPostgresTestSASLInitial(server)
			header := []byte{'R', 0, 0, 0, 0}
			binary.BigEndian.PutUint32(header[1:], maxPostgresAuthMessageBytes+1)
			_, _ = server.Write(header)
		}()
		if err := runPostgresSCRAMWithNonce(client, "probe", "secret", []byte("SCRAM-SHA-256\x00\x00"), "client-nonce"); err == nil {
			t.Fatal("oversized PostgreSQL SCRAM frame was accepted")
		}
	})
}

func TestAuthenticatePostgresUpstreamCompletesSCRAMAndForwardsAuthOK(t *testing.T) {
	upstreamServer, upstreamGateway := net.Pipe()
	defer upstreamServer.Close()
	defer upstreamGateway.Close()
	clientGateway, clientPeer := net.Pipe()
	defer clientGateway.Close()
	defer clientPeer.Close()

	serverResult := make(chan error, 1)
	go func() {
		if err := writePostgresMessage(
			upstreamServer,
			'R',
			append([]byte{0, 0, 0, 10}, []byte("SCRAM-SHA-256\x00\x00")...),
		); err != nil {
			serverResult <- err
			return
		}
		if err := servePostgresSCRAMTest(upstreamServer, postgresSCRAMTestOptions{}); err != nil {
			serverResult <- err
			return
		}
		for _, message := range []postgresWireMessage{
			{kind: 'R', payload: []byte{0, 0, 0, 0}},
			{kind: 'S', payload: []byte("server_version\x0016.0\x00")},
			{kind: 'K', payload: []byte{0, 0, 0, 42, 1, 2, 3, 4}},
			{kind: 'Z', payload: []byte{'I'}},
		} {
			if err := writePostgresMessage(upstreamServer, message.kind, message.payload); err != nil {
				serverResult <- err
				return
			}
		}
		serverResult <- nil
	}()

	type authenticationResult struct {
		startup postgresUpstreamStartup
		err     error
	}
	registered := make(chan postgresCancelKey, 1)
	cleanupCalls := 0
	authResult := make(chan authenticationResult, 1)
	go func() {
		startup, err := authenticatePostgresUpstream(
			upstreamGateway,
			clientGateway,
			"probe",
			"secret",
			func(key postgresCancelKey) func() {
				registered <- key
				return func() {
					cleanupCalls++
				}
			},
		)
		authResult <- authenticationResult{startup: startup, err: err}
	}()

	var kinds []byte
	for {
		message, err := readPostgresMessage(clientPeer, maxPostgresAuthMessageBytes)
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, message.kind)
		if message.kind == 'K' {
			select {
			case key := <-registered:
				if key.processID != 42 ||
					key.secret != string([]byte{1, 2, 3, 4}) {
					t.Fatalf("registered BackendKeyData = %#v", key)
				}
			default:
				t.Fatal("BackendKeyData reached client before cancellation route registration")
			}
		}
		if message.kind == 'Z' {
			break
		}
	}
	if string(kinds) != "RSKZ" {
		t.Fatalf("client startup message kinds = %q, want RSKZ", kinds)
	}
	result := <-authResult
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.startup.cancelCleanup == nil {
		t.Fatal("successful PostgreSQL startup did not return cancellation cleanup")
	}
	result.startup.cancelCleanup()
	if cleanupCalls != 1 {
		t.Fatalf("cancellation cleanup calls = %d, want 1", cleanupCalls)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticatePostgresUpstreamCleansCancelRouteOnStartupFailure(t *testing.T) {
	upstreamServer, upstreamGateway := net.Pipe()
	defer upstreamServer.Close()
	defer upstreamGateway.Close()
	clientGateway, clientPeer := net.Pipe()
	defer clientGateway.Close()
	defer clientPeer.Close()
	go func() {
		_, _ = io.Copy(io.Discard, clientPeer)
	}()

	serverResult := make(chan error, 1)
	go func() {
		for _, message := range []postgresWireMessage{
			{kind: 'R', payload: []byte{0, 0, 0, 0}},
			{kind: 'K', payload: []byte{0, 0, 0, 42, 1, 2, 3, 4}},
			{kind: 'Z', payload: []byte{'X'}},
		} {
			if err := writePostgresMessage(upstreamServer, message.kind, message.payload); err != nil {
				serverResult <- err
				return
			}
		}
		serverResult <- nil
	}()

	cleanupCalls := 0
	_, err := authenticatePostgresUpstream(
		upstreamGateway,
		clientGateway,
		"probe",
		"secret",
		func(postgresCancelKey) func() {
			return func() {
				cleanupCalls++
			}
		},
	)
	if err == nil || !strings.Contains(err.Error(), "ReadyForQuery") {
		t.Fatalf("malformed startup result = %v", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("failed startup cancellation cleanup calls = %d, want 1", cleanupCalls)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

type postgresSCRAMTestOptions struct {
	iterations    int
	badVerifier   bool
	nonceMismatch bool
	serverError   bool
	password      string
}

func servePostgresSCRAMTest(conn net.Conn, options postgresSCRAMTestOptions) error {
	clientFirst, clientFirstBare, err := readPostgresTestSASLInitial(conn)
	if err != nil {
		return err
	}
	if strings.Contains(clientFirst, "\x00") {
		return errors.New("client-first response contains an unexpected terminator")
	}
	attributes, err := parsePostgresSCRAMAttributes(clientFirstBare)
	if err != nil {
		return err
	}
	clientNonce := attributes["r"]
	combinedNonce := clientNonce + "-server"
	if options.nonceMismatch {
		combinedNonce = "different-server-nonce"
	}
	iterations := options.iterations
	if iterations == 0 {
		iterations = minPostgresSCRAMIterations
	}
	salt := []byte("postgres16-salt")
	serverFirst := "r=" + combinedNonce + ",s=" + base64.StdEncoding.EncodeToString(salt) +
		",i=" + decimalString(iterations)
	continueMessage := postgresTestMessage('R', append([]byte{0, 0, 0, 11}, serverFirst...))
	writeFragments(conn, continueMessage, 2, 3, 1, 4)

	if options.nonceMismatch || iterations < minPostgresSCRAMIterations || iterations > maxPostgresSCRAMIterations {
		return nil
	}
	finalMessage, err := readPostgresMessage(conn, maxPostgresAuthMessageBytes)
	if err != nil {
		return err
	}
	if finalMessage.kind != 'p' {
		return errors.New("client SCRAM final response is not a PasswordMessage")
	}
	if len(finalMessage.payload) == 0 || finalMessage.payload[len(finalMessage.payload)-1] == 0 {
		return errors.New("client SCRAM final response has an unexpected terminator")
	}
	clientFinal := string(finalMessage.payload)
	proofPosition := strings.LastIndex(clientFinal, ",p=")
	if proofPosition < 0 {
		return errors.New("client SCRAM final response has no proof")
	}
	clientFinalWithoutProof := clientFinal[:proofPosition]
	authMessage := clientFirstBare + "," + serverFirst + "," + clientFinalWithoutProof
	password := options.password
	if password == "" {
		password = "secret"
	}
	verifier := postgresSCRAMTestVerifier(password, salt, iterations, authMessage)
	if options.badVerifier {
		verifier[0] ^= 0xff
	}
	serverFinal := "v=" + base64.StdEncoding.EncodeToString(verifier)
	if options.serverError {
		serverFinal = "e=invalid-proof"
	}
	final := postgresTestMessage('R', append([]byte{0, 0, 0, 12}, serverFinal...))
	writeFragments(conn, final, 1, 2, 2, 3)
	return nil
}

func readPostgresTestSASLInitial(conn net.Conn) (string, string, error) {
	message, err := readPostgresMessage(conn, maxPostgresAuthMessageBytes)
	if err != nil {
		return "", "", err
	}
	if message.kind != 'p' {
		return "", "", errors.New("SCRAM initial response is not a PasswordMessage")
	}
	mechanismEnd := indexPostgresNUL(message.payload, 0)
	if mechanismEnd < 0 || string(message.payload[:mechanismEnd]) != "SCRAM-SHA-256" {
		return "", "", errors.New("SCRAM initial response has an invalid mechanism")
	}
	lengthPosition := mechanismEnd + 1
	if len(message.payload) < lengthPosition+4 {
		return "", "", errors.New("SCRAM initial response has no response length")
	}
	responseLength := int(binary.BigEndian.Uint32(message.payload[lengthPosition : lengthPosition+4]))
	response := message.payload[lengthPosition+4:]
	if responseLength != len(response) {
		return "", "", errors.New("SCRAM initial response length mismatch")
	}
	clientFirst := string(response)
	if !strings.HasPrefix(clientFirst, "n,,") {
		return "", "", errors.New("SCRAM client-first response has an invalid GS2 header")
	}
	return clientFirst, strings.TrimPrefix(clientFirst, "n,,"), nil
}

func postgresSCRAMTestVerifier(password string, salt []byte, iterations int, authMessage string) []byte {
	saltedPassword := PBKDF2Key(normalizePostgresSCRAMPassword(password), salt, iterations, sha256.Size)
	serverKey := hmacSHA256Test(saltedPassword, []byte("Server Key"))
	return hmacSHA256Test(serverKey, []byte(authMessage))
}

func hmacSHA256Test(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func decimalString(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}
